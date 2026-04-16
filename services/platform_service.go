package services

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"
	"vm-platform/models"
)

var portCounter = 2222

// initPortCounter scans all registered VMs to find the highest used port and sets portCounter above it
func initPortCounter(vbox *VBoxService) {
	vms, err := vbox.ListVMs()
	if err != nil {
		return
	}
	maxPort := 2221
	for _, vm := range vms {
		port, err := vbox.GetNATPort(vm)
		if err != nil || port == "" {
			continue
		}
		var p int
		fmt.Sscanf(port, "%d", &p)
		if p > maxPort {
			maxPort = p
		}
	}
	portCounter = maxPort + 1
}

type PlatformService struct {
	VBox     *VBoxService
	SSH      *SSHService
	BaseVMs  map[string]*models.BaseVM
	Disks    map[string]*models.MultiAttachDisk
	UserVMs  map[string]*models.UserVM
	mu       sync.RWMutex
	dataFile string
}

type PlatformData struct {
	BaseVMs map[string]*models.BaseVM          `json:"base_vms"`
	Disks   map[string]*models.MultiAttachDisk `json:"disks"`
	UserVMs map[string]*models.UserVM          `json:"user_vms"`
}

func NewPlatformService() *PlatformService {
	vbox := NewVBoxService()
	sshSvc := NewSSHService(vbox)
	usr, _ := user.Current()
	dataDir := filepath.Join(usr.HomeDir, ".vm-platform")
	os.MkdirAll(dataDir, 0755)
	ps := &PlatformService{
		VBox: vbox, SSH: sshSvc,
		BaseVMs:  make(map[string]*models.BaseVM),
		Disks:    make(map[string]*models.MultiAttachDisk),
		UserVMs:  make(map[string]*models.UserVM),
		dataFile: filepath.Join(dataDir, "platform_data.json"),
	}
	ps.loadState()
	initPortCounter(vbox)
	return ps
}

func (p *PlatformService) saveState() {
	data := PlatformData{BaseVMs: p.BaseVMs, Disks: p.Disks, UserVMs: p.UserVMs}
	jd, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile(p.dataFile, jd, 0644)
}

func (p *PlatformService) loadState() {
	d, err := os.ReadFile(p.dataFile)
	if err != nil {
		return
	}
	var pd PlatformData
	json.Unmarshal(d, &pd)
	if pd.BaseVMs != nil {
		p.BaseVMs = pd.BaseVMs
	}
	if pd.Disks != nil {
		p.Disks = pd.Disks
	}
	if pd.UserVMs != nil {
		p.UserVMs = pd.UserVMs
	}
}

func (p *PlatformService) AddBaseVM(name, desc string) (*models.BaseVM, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.VBox.VMExists(name) {
		return nil, fmt.Errorf("la VM no existe")
	}
	vm := &models.BaseVM{Name: name, Description: desc, CreatedAt: time.Now()}
	p.BaseVMs[name] = vm
	p.saveState()
	return vm, nil
}

func (p *PlatformService) CreateRootKeys(vmName, rootPass string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	baseVM, exists := p.BaseVMs[vmName]
	if !exists {
		return fmt.Errorf("base no encontrada")
	}
	port, _ := p.VBox.GetNATPort(vmName)
	if port == "" {
		p.VBox.runCommand("modifyvm", vmName, "--natpf1", fmt.Sprintf("ssh,tcp,,%d,,22", portCounter))
		portCounter++
	}
	if err := p.SSH.DeployRootKeys(vmName, rootPass); err != nil {
		return err
	}
	baseVM.KeysCreated = true
	p.saveState()
	return nil
}

func (p *PlatformService) CreateMultiAttachDisk(baseVMName string) (*models.MultiAttachDisk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	baseVM, exists := p.BaseVMs[baseVMName]
	if !exists {
		return nil, fmt.Errorf("VM no encontrada")
	}

	attachedDiskPath, err := p.VBox.GetVMDiskPath(baseVMName)
	if err != nil {
		return nil, err
	}

	baseDiskPath, err := p.VBox.GetBaseMediumPath(attachedDiskPath)
	if err != nil {
		return nil, fmt.Errorf("error al encontrar disco base: %v", err)
	}

	state, _ := p.VBox.GetVMState(baseVMName)
	if state != "poweroff" {
		p.VBox.StopVM(baseVMName)
		for i := 0; i < 6; i++ {
			time.Sleep(5 * time.Second)
			state, _ = p.VBox.GetVMState(baseVMName)
			if state == "poweroff" {
				break
			}
		}
	}

	if err := p.VBox.DetachDisk(baseVMName, attachedDiskPath); err != nil {
		return nil, fmt.Errorf("error al desconectar disco: %v", err)
	}
	time.Sleep(2 * time.Second)

	if err := p.VBox.ConvertDiskToMultiAttach(baseDiskPath); err != nil {
		p.VBox.AttachDisk(baseVMName, attachedDiskPath)
		return nil, err
	}

	p.VBox.AttachDisk(baseVMName, baseDiskPath)
	diskName := "disk-" + baseVMName
	disk := &models.MultiAttachDisk{Name: diskName, BaseVMName: baseVMName, DiskPath: baseDiskPath, Connected: true, CreatedAt: time.Now()}
	baseVM.DiskCreated = true
	p.Disks[diskName] = disk
	p.saveState()
	return disk, nil
}

func (p *PlatformService) DisconnectDisk(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, _ := p.Disks[name]
	p.VBox.DetachDisk(d.BaseVMName, d.DiskPath)
	d.Connected = false
	p.saveState()
	return nil
}

func (p *PlatformService) ConnectDisk(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, _ := p.Disks[name]
	p.VBox.AttachDisk(d.BaseVMName, d.DiskPath)
	d.Connected = true
	p.saveState()
	return nil
}

func (p *PlatformService) DeleteDisk(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.Disks[name]
	if !ok {
		return fmt.Errorf("disco no encontrado: %s", name)
	}
	p.VBox.DetachDisk(d.BaseVMName, d.DiskPath)
	p.VBox.DeleteDisk(d.DiskPath)
	if b, ok := p.BaseVMs[d.BaseVMName]; ok {
		b.DiskCreated = false
	}
	delete(p.Disks, name)
	p.saveState()
	return nil
}

func (p *PlatformService) CreateUserVM(name, desc, diskName string) (*models.UserVM, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	disk, _ := p.Disks[diskName]
	if err := p.VBox.CreateVMFromDisk(name, disk.DiskPath, disk.BaseVMName); err != nil {
		return nil, err
	}
	port, _ := p.VBox.GetNATPort(name)
	if port == "" {
		p.VBox.runCommand("modifyvm", name, "--natpf1", fmt.Sprintf("ssh,tcp,,%d,,22", portCounter))
		portCounter++
	}
	uvm := &models.UserVM{Name: name, Description: desc, DiskName: diskName, BaseVMName: disk.BaseVMName, CreatedAt: time.Now()}
	p.UserVMs[name] = uvm
	p.saveState()
	return uvm, nil
}

func (p *PlatformService) CreateVMUser(vmName, user, pass, rootPass string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	uvm, _ := p.UserVMs[vmName]

	// Deploy root keys for the user VM so deploy/service actions work via key auth
	if !p.SSH.KeysExist(vmName, "root") {
		if err := p.SSH.DeployRootKeys(vmName, rootPass); err != nil {
			return fmt.Errorf("error al desplegar llaves root en VM de usuario: %v", err)
		}
	}

	if err := p.SSH.DeployUserKeys(vmName, user, pass, rootPass); err != nil {
		return err
	}
	uvm.UserCreated = true
	uvm.Username = user
	p.saveState()
	return nil
}

// RepairKeys regenerates and redeploys both root and user SSH keys for a user VM
func (p *PlatformService) RepairKeys(vmName, rootPass string) error {
	p.mu.RLock()
	uvm, exists := p.UserVMs[vmName]
	p.mu.RUnlock()
	if !exists {
		return fmt.Errorf("VM de usuario no encontrada: %s", vmName)
	}
	return p.SSH.RepairAllKeys(vmName, uvm.Username, rootPass)
}

// DeployRootKeysToUserVM deploys root SSH keys to an existing user VM using root password
func (p *PlatformService) DeployRootKeysToUserVM(vmName, rootPass string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.UserVMs[vmName]; !exists {
		return fmt.Errorf("VM de usuario no encontrada: %s", vmName)
	}
	return p.SSH.DeployRootKeys(vmName, rootPass)
}

func (p *PlatformService) GetDashboardData() PlatformData {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return PlatformData{BaseVMs: p.BaseVMs, Disks: p.Disks, UserVMs: p.UserVMs}
}

func (p *PlatformService) GetRootKeyPath(vmName string) (string, error) {
	return p.SSH.GetPrivateKeyPath(vmName, "root"), nil
}

func (p *PlatformService) GetUserKeyPath(vmName string) (string, error) {
	uvm, _ := p.UserVMs[vmName]
	return p.SSH.GetPrivateKeyPath(vmName, uvm.Username), nil
}

func (p *PlatformService) DeleteUserVM(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.VBox.DeleteVM(name)
	delete(p.UserVMs, name)
	p.saveState()
	return nil
}
