package services

import (
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

type VBoxService struct {
	VBoxManagePath string
	DefaultFolder  string
}

func NewVBoxService() *VBoxService {
	vboxPath := "VBoxManage"
	if runtime.GOOS == "windows" {
		vboxPath = `C:\Program Files\Oracle\VirtualBox\VBoxManage.exe`
	}
	usr, _ := user.Current()
	defaultFolder := filepath.Join(usr.HomeDir, "VirtualBox VMs")
	return &VBoxService{VBoxManagePath: vboxPath, DefaultFolder: defaultFolder}
}

func (v *VBoxService) runCommand(args ...string) (string, error) {
	cmd := exec.Command(v.VBoxManagePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("VBoxManage error: %s - %v", string(output), err)
	}
	return string(output), nil
}

func (v *VBoxService) ListVMs() ([]string, error) {
	output, err := v.runCommand("list", "vms")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var vms []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\"")
		if len(parts) > 1 {
			vms = append(vms, parts[1])
		}
	}
	return vms, nil
}

func (v *VBoxService) VMExists(name string) bool {
	_, err := v.runCommand("showvminfo", name)
	return err == nil
}

func (v *VBoxService) GetVMState(name string) (string, error) {
	output, err := v.runCommand("showvminfo", name, "--machinereadable")
	if err != nil {
		return "unknown", err
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "VMState=") {
			return strings.Trim(strings.Split(line, "=")[1], "\" \r\n"), nil
		}
	}
	return "unknown", nil
}

func (v *VBoxService) StartVM(name string) error {
	_, err := v.runCommand("startvm", name, "--type", "headless")
	return err
}

func (v *VBoxService) StopVM(name string) error {
	_, err := v.runCommand("controlvm", name, "poweroff")
	return err
}

func (v *VBoxService) GetVMDiskPath(vmName string) (string, error) {
	output, err := v.runCommand("showvminfo", vmName, "--machinereadable")
	if err != nil {
		return "", err
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, ".vdi") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) < 2 {
				continue
			}
			path := strings.Trim(parts[1], "\" \r\n")
			path = strings.ReplaceAll(path, "\\\\", "\\")
			return path, nil
		}
	}
	return "", fmt.Errorf("disco no encontrado")
}

func (v *VBoxService) GetDiskAttachment(vmName, diskPath string) (ctl, port, dev string, err error) {
	output, err := v.runCommand("showvminfo", vmName, "--machinereadable")
	if err != nil {
		return "", "", "", err
	}
	normalizedDisk := strings.ToLower(filepath.Clean(strings.ReplaceAll(diskPath, "\\\\", "\\")))
	for _, line := range strings.Split(output, "\n") {
		normalizedLine := strings.ToLower(strings.ReplaceAll(line, "\\\\", "\\"))
		if strings.Contains(normalizedLine, normalizedDisk) {
			key := strings.Trim(strings.SplitN(line, "=", 2)[0], "\"")
			parts := strings.Split(key, "-")
			if len(parts) >= 3 {
				return parts[0], parts[1], parts[2], nil
			}
		}
	}
	return "", "", "", fmt.Errorf("no conectado")
}

func (v *VBoxService) DetachDisk(vmName, diskPath string) error {
	ctl, port, dev, err := v.GetDiskAttachment(vmName, diskPath)
	if err != nil {
		return nil
	}
	_, err = v.runCommand("storageattach", vmName, "--storagectl", ctl, "--port", port, "--device", dev, "--medium", "none")
	if err != nil {
		return fmt.Errorf("error al desconectar disco del controlador %s puerto %s dispositivo %s: %v", ctl, port, dev, err)
	}
	return nil
}

func (v *VBoxService) AttachDisk(vmName, diskPath string) error {
	output, _ := v.runCommand("showvminfo", vmName, "--machinereadable")
	ctl := "SATA"
	if strings.Contains(output, "SATA Controller") {
		ctl = "SATA Controller"
	}
	_, err := v.runCommand("storageattach", vmName, "--storagectl", ctl, "--port", "0", "--device", "0", "--type", "hdd", "--medium", diskPath)
	return err
}

func (v *VBoxService) ConvertDiskToMultiAttach(diskPath string) error {
	_, err := v.runCommand("modifymedium", "disk", diskPath, "--type", "multiattach")
	return err
}

func (v *VBoxService) GetBaseMediumPath(diskPath string) (string, error) {
	for {
		output, err := v.runCommand("showmediuminfo", "disk", diskPath)
		if err != nil {
			return diskPath, nil
		}
		var parentUUID string
		for _, line := range strings.Split(output, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Parent UUID:") {
				parentUUID = strings.TrimSpace(strings.TrimPrefix(trimmed, "Parent UUID:"))
				break
			}
		}
		if parentUUID == "" || parentUUID == "base" {
			return diskPath, nil
		}
		parentPath, err := v.getDiskPathByUUID(parentUUID)
		if err != nil {
			return "", fmt.Errorf("no se pudo encontrar el disco padre %s: %v", parentUUID, err)
		}
		diskPath = parentPath
	}
}

func (v *VBoxService) getDiskPathByUUID(uuid string) (string, error) {
	output, err := v.runCommand("showmediuminfo", "disk", uuid)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Location:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Location:")), nil
		}
	}
	return "", fmt.Errorf("location no encontrado para UUID %s", uuid)
}

func (v *VBoxService) CreateVMFromDisk(name, diskPath, baseName string) error {
	v.runCommand("createvm", "--name", name, "--register")
	v.runCommand("modifyvm", name, "--memory", "1024", "--nic1", "nat")
	v.runCommand("storagectl", name, "--name", "SATA", "--add", "sata")
	_, err := v.runCommand("storageattach", name, "--storagectl", "SATA", "--port", "0", "--device", "0", "--type", "hdd", "--medium", diskPath)
	return err
}

func (v *VBoxService) DeleteVM(vmName string) error {
	v.StopVM(vmName)
	_, err := v.runCommand("unregistervm", vmName, "--delete")
	return err
}

func (v *VBoxService) DeleteDisk(diskPath string) error {
	v.runCommand("modifymedium", "disk", diskPath, "--type", "normal")
	_, err := v.runCommand("closemedium", "disk", diskPath, "--delete")
	return err
}

func (v *VBoxService) GetNATPort(vmName string) (string, error) {
	output, err := v.runCommand("showvminfo", vmName, "--machinereadable")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Forwarding") && strings.Contains(line, "ssh") {
			parts := strings.Split(line, ",")
			if len(parts) >= 4 {
				return parts[3], nil
			}
		}
	}
	return "", nil
}
