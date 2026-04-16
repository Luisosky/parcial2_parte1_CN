package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"vm-platform/models"

	"golang.org/x/crypto/ssh"
)

// DeployService handles application deployment and systemd service management
type DeployService struct {
	VBox        *VBoxService
	SSHSvc      *SSHService
	Deployments map[string]*models.DeploymentInfo // key: vmName
	mu          sync.RWMutex
	dataFile    string
}

// NewDeployService creates a new deploy service
func NewDeployService(vbox *VBoxService, sshSvc *SSHService) *DeployService {
	usr, _ := user.Current()
	dataDir := filepath.Join(usr.HomeDir, ".vm-platform")
	os.MkdirAll(dataDir, 0755)

	ds := &DeployService{
		VBox:        vbox,
		SSHSvc:      sshSvc,
		Deployments: make(map[string]*models.DeploymentInfo),
		dataFile:    filepath.Join(dataDir, "deployments.json"),
	}
	ds.loadState()
	return ds
}

func (d *DeployService) saveState() {
	jd, _ := json.MarshalIndent(d.Deployments, "", "  ")
	os.WriteFile(d.dataFile, jd, 0644)
}

func (d *DeployService) loadState() {
	data, err := os.ReadFile(d.dataFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &d.Deployments)
}

// GetDeployment returns deployment info for a VM
func (d *DeployService) GetDeployment(vmName string) *models.DeploymentInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Deployments[vmName]
}

// GetAllDeployments returns all deployments
func (d *DeployService) GetAllDeployments() map[string]*models.DeploymentInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]*models.DeploymentInfo)
	for k, v := range d.Deployments {
		result[k] = v
	}
	return result
}

// DeployApplication uploads a zip file to the VM and extracts it
func (d *DeployService) DeployApplication(vmName, username, destFolder, execCommand, execParams, password string, zipData []byte, zipFileName string) (*models.DeploymentInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Ensure the VM is running
	state, _ := d.VBox.GetVMState(vmName)
	if state != "running" {
		if err := d.VBox.StartVM(vmName); err != nil {
			return nil, fmt.Errorf("error al iniciar la VM: %v", err)
		}
		time.Sleep(30 * time.Second)
	}

	port, err := d.VBox.GetNATPort(vmName)
	if err != nil {
		return nil, fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	// Get the private key path for the user
	keyPath := d.SSHSvc.GetPrivateKeyPath(vmName, username)

	// 0. Stop existing service if re-deploying (avoids "Text file busy" error)
	if existing, ok := d.Deployments[vmName]; ok && existing.ServiceCreated && existing.ServiceName != "" {
		rootKeyPath := d.SSHSvc.GetPrivateKeyPath(vmName, "root")
		rootPwd := existing.RootPassword
		stopCmd := fmt.Sprintf("systemctl stop %s 2>/dev/null || true", existing.ServiceName)
		runSSHCommandWithKeyOrPassword(port, "root", rootKeyPath, rootPwd, stopCmd)
	}

	// 1. Create the destination folder
	mkdirCmd := fmt.Sprintf("mkdir -p %s", destFolder)
	if userErr := runSSHCommandWithKeyOrPassword(port, username, keyPath, password, mkdirCmd); userErr != nil {
		// Try with root if user can't create the folder
		rootKeyPath := d.SSHSvc.GetPrivateKeyPath(vmName, "root")
		if _, statErr := os.Stat(rootKeyPath); os.IsNotExist(statErr) {
			return nil, fmt.Errorf("SSH como '%s' falló (%v) y no hay llave root disponible. Primero despliega las llaves root en la VM de usuario via /api/user-vm/deploy-root-keys", username, userErr)
		}
		mkdirCmdRoot := fmt.Sprintf("mkdir -p %s && chown -R %s:%s %s", destFolder, username, username, destFolder)
		if err2 := runSSHCommandWithKeyOrPassword(port, "root", rootKeyPath, "", mkdirCmdRoot); err2 != nil {
			return nil, fmt.Errorf("SSH como '%s' falló (%v); SSH como root también falló: %v", username, userErr, err2)
		}
	}

	// 2. Upload the zip file via SSH pipe
	remotePath := fmt.Sprintf("%s/%s", destFolder, zipFileName)
	if err := d.uploadFileViaSSH(port, username, keyPath, password, zipData, remotePath); err != nil {
		return nil, fmt.Errorf("error al subir archivo zip: %v", err)
	}

	// 3. Extract the zip on the remote machine (try unzip, then python3, then busybox)
	extractCmd := fmt.Sprintf(
		`cd %s && if command -v unzip >/dev/null 2>&1; then unzip -o %s; elif command -v python3 >/dev/null 2>&1; then python3 -c "import zipfile,sys; zipfile.ZipFile('%s').extractall('.')"; elif command -v busybox >/dev/null 2>&1; then busybox unzip -o %s; else echo 'No se encontró unzip, python3 ni busybox' && exit 1; fi`,
		destFolder, zipFileName, zipFileName, zipFileName)
	if err := runSSHCommandWithKeyOrPassword(port, username, keyPath, password, extractCmd); err != nil {
		return nil, fmt.Errorf("error al extraer zip: %v", err)
	}

	// 4. Make the executable file executable
	chmodCmd := fmt.Sprintf("chmod +x %s/%s", destFolder, execCommand)
	runSSHCommandWithKeyOrPassword(port, username, keyPath, password, chmodCmd)

	// Determine log file name (use "/" for remote Linux paths, not filepath.Join which uses "\" on Windows)
	logFile := destFolder + "/app_log.txt"
	if strings.HasSuffix(destFolder, "/") {
		logFile = destFolder + "app_log.txt"
	}

	// 5. Quick test: start, wait briefly, then kill
	testCmd := fmt.Sprintf("cd %s && nohup ./%s %s > /dev/null 2>&1 & sleep 2 && pkill -f '%s/%s' || true", destFolder, execCommand, execParams, destFolder, execCommand)
	runSSHCommandWithKeyOrPassword(port, username, keyPath, password, testCmd)

	info := &models.DeploymentInfo{
		VMName:      vmName,
		Username:    username,
		Password:    password,
		ZipFileName: zipFileName,
		DestFolder:  destFolder,
		ExecCommand: execCommand,
		ExecParams:  execParams,
		LogFile:     logFile,
		Deployed:    true,
		CreatedAt:   time.Now(),
	}
	d.Deployments[vmName] = info
	d.saveState()

	return info, nil
}

// uploadFileViaSSH uploads file data to the remote machine over SSH
func (d *DeployService) uploadFileViaSSH(port, username, keyPath, password string, data []byte, remotePath string) error {
	var authMethods []ssh.AuthMethod

	// Try to load the key
	if keyBytes, err := os.ReadFile(keyPath); err == nil {
		if signer, err := ssh.ParsePrivateKey(keyBytes); err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// Add password fallback if provided
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	if len(authMethods) == 0 {
		return fmt.Errorf("no hay métodos de autenticación disponibles (sin llave ni contraseña)")
	}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	client, err := ssh.Dial("tcp", "127.0.0.1:"+port, config)
	if err != nil {
		return fmt.Errorf("error de conexión SSH: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("error de sesión SSH: %v", err)
	}
	defer session.Close()

	// Use cat to write the file
	session.Stdin = bytes.NewReader(data)
	cmd := fmt.Sprintf("cat > '%s'", remotePath)
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("error al escribir archivo: %v", err)
	}

	return nil
}

// CreateSystemdService creates a systemd service file on the VM
func (d *DeployService) CreateSystemdService(vmName, serviceName, rootPassword string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	info, ok := d.Deployments[vmName]
	if !ok || !info.Deployed {
		return fmt.Errorf("no hay aplicación desplegada en la VM '%s'", vmName)
	}

	port, err := d.VBox.GetNATPort(vmName)
	if err != nil {
		return fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	// Use root to create the systemd service
	rootKeyPath := d.SSHSvc.GetPrivateKeyPath(vmName, "root")

	// Build the systemd unit file content
	execStart := fmt.Sprintf("%s/%s", info.DestFolder, info.ExecCommand)
	if info.ExecParams != "" {
		execStart += " " + info.ExecParams
	}

	unitContent := fmt.Sprintf(`[Unit]
Description=Servicio gestionado - %s
After=network.target

[Service]
Type=simple
ExecStart=%s
WorkingDirectory=%s
User=%s
Restart=always
RestartSec=2
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=multi-user.target
`, serviceName, execStart, info.DestFolder, info.Username, info.LogFile, info.LogFile)

	// Upload the service file
	serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)

	// Write unit file via SSH — ensure the log file exists and is writable
	touchCmd := fmt.Sprintf("touch %s && chown %s:%s %s", info.LogFile, info.Username, info.Username, info.LogFile)
	runSSHCommandWithKeyOrPassword(port, "root", rootKeyPath, rootPassword, touchCmd)

	// Write unit file via SSH
	escapedContent := strings.ReplaceAll(unitContent, "'", "'\\''")
	writeCmd := fmt.Sprintf("echo '%s' > %s", escapedContent, serviceFilePath)
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKeyPath, rootPassword, writeCmd); err != nil {
		return fmt.Errorf("error al crear archivo de servicio: %v", err)
	}

	// Reload systemd daemon
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKeyPath, rootPassword, "systemctl daemon-reload"); err != nil {
		return fmt.Errorf("error al recargar systemd: %v", err)
	}

	info.ServiceName = serviceName
	info.ServiceFile = serviceFilePath
	info.ServiceCreated = true
	if rootPassword != "" {
		info.RootPassword = rootPassword
	}
	d.saveState()

	return nil
}

// ServiceAction performs an action on the systemd service
func (d *DeployService) ServiceAction(vmName, action, rootPassword string) error {
	d.mu.RLock()
	info, ok := d.Deployments[vmName]
	d.mu.RUnlock()

	if !ok || !info.ServiceCreated {
		return fmt.Errorf("no hay servicio configurado en la VM '%s'", vmName)
	}

	port, err := d.VBox.GetNATPort(vmName)
	if err != nil {
		return fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	rootKeyPath := d.SSHSvc.GetPrivateKeyPath(vmName, "root")
	if rootPassword == "" {
		rootPassword = info.RootPassword
	}

	var cmd string
	switch action {
	case "start":
		cmd = fmt.Sprintf("systemctl start %s", info.ServiceName)
	case "stop":
		cmd = fmt.Sprintf("systemctl stop %s", info.ServiceName)
	case "restart":
		cmd = fmt.Sprintf("systemctl restart %s", info.ServiceName)
	case "enable":
		cmd = fmt.Sprintf("systemctl enable %s", info.ServiceName)
	case "disable":
		cmd = fmt.Sprintf("systemctl disable %s", info.ServiceName)
	default:
		return fmt.Errorf("acción no válida: %s", action)
	}

	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKeyPath, rootPassword, cmd); err != nil {
		return fmt.Errorf("error al ejecutar '%s': %v", action, err)
	}

	return nil
}

// GetServiceStatus returns the current status of the service
func (d *DeployService) GetServiceStatus(vmName string) (*models.ServiceStatus, error) {
	d.mu.RLock()
	info, ok := d.Deployments[vmName]
	d.mu.RUnlock()

	if !ok || !info.ServiceCreated {
		return nil, fmt.Errorf("no hay servicio configurado en la VM '%s'", vmName)
	}

	port, err := d.VBox.GetNATPort(vmName)
	if err != nil {
		return nil, fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	rootKeyPath := d.SSHSvc.GetPrivateKeyPath(vmName, "root")
	rootPwd := info.RootPassword

	// Get active state
	activeState := getSSHOutputWithPassword(port, "root", rootKeyPath, rootPwd, fmt.Sprintf("systemctl is-active %s 2>/dev/null || echo 'inactive'", info.ServiceName))
	enabledState := getSSHOutputWithPassword(port, "root", rootKeyPath, rootPwd, fmt.Sprintf("systemctl is-enabled %s 2>/dev/null || echo 'disabled'", info.ServiceName))
	statusText := getSSHOutputWithPassword(port, "root", rootKeyPath, rootPwd, fmt.Sprintf("systemctl status %s 2>/dev/null || true", info.ServiceName))

	return &models.ServiceStatus{
		VMName:      vmName,
		ServiceName: info.ServiceName,
		Active:      strings.TrimSpace(activeState),
		Enabled:     strings.TrimSpace(enabledState),
		StatusText:  statusText,
		Running:     strings.TrimSpace(activeState) == "active",
		IsEnabled:   strings.TrimSpace(enabledState) == "enabled",
	}, nil
}

// GetLogContent returns the current content of the log file
func (d *DeployService) GetLogContent(vmName string, lines int) (string, error) {
	d.mu.RLock()
	info, ok := d.Deployments[vmName]
	d.mu.RUnlock()

	if !ok || !info.Deployed {
		return "", fmt.Errorf("no hay aplicación desplegada en la VM '%s'", vmName)
	}

	port, err := d.VBox.GetNATPort(vmName)
	if err != nil {
		return "", fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	rootKeyPath := d.SSHSvc.GetPrivateKeyPath(vmName, "root")
	rootPwd := info.RootPassword
	userPwd := info.Password

	if lines <= 0 {
		lines = 50
	}

	// Try reading logs via journalctl first (works even if file doesn't exist yet), then fall back to log file
	var output string
	if info.ServiceCreated && info.ServiceName != "" {
		journalCmd := fmt.Sprintf("journalctl -u %s -n %d --no-pager 2>/dev/null || true", info.ServiceName, lines)
		output = getSSHOutputWithPassword(port, "root", rootKeyPath, rootPwd, journalCmd)
	}
	if output == "" {
		cmd := fmt.Sprintf("tail -n %d %s 2>/dev/null || echo '[Archivo aún no creado]'", lines, info.LogFile)
		keyPath := d.SSHSvc.GetPrivateKeyPath(vmName, info.Username)
		output = getSSHOutputWithPassword(port, info.Username, keyPath, userPwd, cmd)
		if output == "" {
			output = getSSHOutputWithPassword(port, "root", rootKeyPath, rootPwd, cmd)
		}
	}
	return output, nil
}

// StreamLog opens a persistent SSH session and streams tail -f output
func (d *DeployService) StreamLog(vmName string, outputChan chan<- string, stopChan <-chan struct{}) error {
	d.mu.RLock()
	info, ok := d.Deployments[vmName]
	d.mu.RUnlock()

	if !ok || !info.Deployed {
		return fmt.Errorf("no hay aplicación desplegada")
	}

	port, err := d.VBox.GetNATPort(vmName)
	if err != nil {
		return err
	}

	rootKeyPath := d.SSHSvc.GetPrivateKeyPath(vmName, "root")
	var authMethods []ssh.AuthMethod

	if keyBytes, err := os.ReadFile(rootKeyPath); err == nil {
		if signer, err := ssh.ParsePrivateKey(keyBytes); err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if info.RootPassword != "" {
		authMethods = append(authMethods, ssh.Password(info.RootPassword))
	}
	if len(authMethods) == 0 {
		return fmt.Errorf("no hay métodos de autenticación disponibles")
	}

	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", "127.0.0.1:"+port, config)
	if err != nil {
		return err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return err
	}

	// Use journalctl -f for live streaming if service exists, otherwise tail -f the log file
	var cmd string
	if info.ServiceCreated && info.ServiceName != "" {
		cmd = fmt.Sprintf("journalctl -u %s -f --no-pager 2>/dev/null", info.ServiceName)
	} else {
		cmd = fmt.Sprintf("tail -f %s 2>/dev/null", info.LogFile)
	}
	if err := session.Start(cmd); err != nil {
		session.Close()
		client.Close()
		return err
	}

	// Read output in a goroutine
	go func() {
		defer session.Close()
		defer client.Close()

		buf := make([]byte, 4096)
		for {
			select {
			case <-stopChan:
				session.Signal(ssh.SIGTERM)
				return
			default:
				n, err := stdout.Read(buf)
				if n > 0 {
					outputChan <- string(buf[:n])
				}
				if err != nil {
					return
				}
			}
		}
	}()

	return nil
}

// getSSHOutputWithPassword runs a command and returns its output, with key+password fallback
func getSSHOutputWithPassword(port, username, keyPath, password, command string) string {
	var authMethods []ssh.AuthMethod

	if keyBytes, err := os.ReadFile(keyPath); err == nil {
		if signer, err := ssh.ParsePrivateKey(keyBytes); err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}
	if len(authMethods) == 0 {
		return ""
	}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", "127.0.0.1:"+port, config)
	if err != nil {
		return ""
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return ""
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		// Still return output even on non-zero exit
		return string(output)
	}
	return string(output)
}

// EnsureVMRunning starts the VM if it's not running
func (d *DeployService) EnsureVMRunning(vmName string) error {
	state, _ := d.VBox.GetVMState(vmName)
	if state != "running" {
		if err := d.VBox.StartVM(vmName); err != nil {
			return fmt.Errorf("error al iniciar la VM: %v", err)
		}
		time.Sleep(30 * time.Second)
	}
	return nil
}
