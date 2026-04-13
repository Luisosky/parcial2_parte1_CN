package services

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHService handles SSH key generation and deployment
type SSHService struct {
	KeysDir string
	VBoxSvc *VBoxService
}

// NewSSHService creates a new SSH service
func NewSSHService(vboxSvc *VBoxService) *SSHService {
	usr, _ := user.Current()
	keysDir := filepath.Join(usr.HomeDir, ".vm-platform", "keys")
	os.MkdirAll(keysDir, 0700)

	return &SSHService{
		KeysDir: keysDir,
		VBoxSvc: vboxSvc,
	}
}

// GenerateRSAKeyPair generates an RSA-1024 key pair and saves to disk
func (s *SSHService) GenerateRSAKeyPair(vmName string, username string) (privateKeyPath string, publicKeyPath string, err error) {
	// 1. Generar la llave (1024 bits como pide el parcial)
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return "", "", fmt.Errorf("error al generar llave RSA: %v", err)
	}

	vmKeysDir := filepath.Join(s.KeysDir, vmName)
	os.MkdirAll(vmKeysDir, 0700)

	// 2. Guardar Llave Privada
	privateKeyPath = filepath.Join(vmKeysDir, fmt.Sprintf("id_rsa_%s", username))
	privateKeyFile, err := os.OpenFile(privateKeyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", "", fmt.Errorf("error al crear archivo de llave privada: %v", err)
	}
	defer privateKeyFile.Close()

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	if err := pem.Encode(privateKeyFile, privateKeyPEM); err != nil {
		return "", "", fmt.Errorf("error al codificar llave privada: %v", err)
	}

	// 3. ¡LA MEJORA! Usar la librería oficial de Go para la llave Pública (sin errores de bytes)
	publicRsaKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("error al crear llave pública ssh: %v", err)
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(publicRsaKey)

	publicKeyPath = filepath.Join(vmKeysDir, fmt.Sprintf("id_rsa_%s.pub", username))
	if err := os.WriteFile(publicKeyPath, pubKeyBytes, 0644); err != nil {
		return "", "", fmt.Errorf("error al escribir llave pública: %v", err)
	}

	return privateKeyPath, publicKeyPath, nil
}

// =========================================================================
// FUNCIONES AUXILIARES DE SSH NATIVO
// =========================================================================

func runSSHCommandWithPassword(port string, username string, password string, command string) error {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
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

	output, err := session.CombinedOutput(command)
	if err != nil {
		return fmt.Errorf("ejecución fallida: %s - %v", string(output), err)
	}
	return nil
}

func runSSHCommandWithKey(port string, username string, keyPath string, command string) error {
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("no se pudo leer la llave: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return fmt.Errorf("llave inválida: %v", err)
	}

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", "127.0.0.1:"+port, config)
	if err != nil {
		return fmt.Errorf("error de conexión SSH con llave: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("error de sesión SSH: %v", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		return fmt.Errorf("ejecución fallida: %s - %v", string(output), err)
	}
	return nil
}

// =========================================================================

// DeployRootKeys generates keys and deploys them to the VM for root user
func (s *SSHService) DeployRootKeys(vmName string, rootPassword string) error {
	privateKeyPath, publicKeyPath, err := s.GenerateRSAKeyPair(vmName, "root")
	if err != nil {
		return err
	}

	state, _ := s.VBoxSvc.GetVMState(vmName)
	wasRunning := state == "running"

	if !wasRunning {
		if err := s.VBoxSvc.StartVM(vmName); err != nil {
			return fmt.Errorf("error al iniciar VM: %v", err)
		}
		time.Sleep(30 * time.Second)
	}

	port, err := s.VBoxSvc.GetNATPort(vmName)
	if err != nil {
		return fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	pubKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("error al leer llave pública: %v", err)
	}
	pubKeyStr := strings.TrimSpace(string(pubKeyData))

	remoteCommand := fmt.Sprintf(`mkdir -p /root/.ssh && chmod 700 /root/.ssh && echo "%s" > /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys`, pubKeyStr)

	// 1. Nos conectamos con contraseña e inyectamos la llave (ESTO YA FUNCIONA)
	err = runSSHCommandWithPassword(port, "root", rootPassword, remoteCommand)
	if err != nil {
		return fmt.Errorf("error al desplegar llaves de root: %v", err)
	}

	// 2. Verificamos que la llave inyectada funciona correctamente (ESTO FALLABA, AHORA FUNCIONARÁ)
	err = runSSHCommandWithKey(port, "root", privateKeyPath, "echo OK")
	if err != nil {
		return fmt.Errorf("verificación de llaves falló: %v", err)
	}

	if !wasRunning {
		s.VBoxSvc.StopVM(vmName)
	}

	return nil
}

// DeployUserKeys creates a user in the VM and deploys SSH keys.
// Uses root password authentication since user VMs (multi-attach) boot
// without the root authorized_keys from the base VM's differencing disk.
func (s *SSHService) DeployUserKeys(vmName string, username string, password string, rootPassword string) error {
	state, _ := s.VBoxSvc.GetVMState(vmName)
	wasRunning := state == "running"

	if !wasRunning {
		if err := s.VBoxSvc.StartVM(vmName); err != nil {
			return fmt.Errorf("error al iniciar VM: %v", err)
		}
		time.Sleep(30 * time.Second)
	}

	port, err := s.VBoxSvc.GetNATPort(vmName)
	if err != nil {
		return fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	privateKeyPath, publicKeyPath, err := s.GenerateRSAKeyPair(vmName, username)
	if err != nil {
		return err
	}

	pubKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("error al leer llave pública: %v", err)
	}
	pubKeyStr := strings.TrimSpace(string(pubKeyData))

	remoteCommand := fmt.Sprintf(`useradd -m -s /bin/bash %s && echo "%s:%s" | chpasswd && mkdir -p /home/%s/.ssh && chmod 700 /home/%s/.ssh && echo "%s" > /home/%s/.ssh/authorized_keys && chmod 600 /home/%s/.ssh/authorized_keys && chown -R %s:%s /home/%s/.ssh`,
		username, username, password, username, username, pubKeyStr, username, username, username, username, username)

	// Conectar con contraseña root (multi-attach: cada VM arranca sin el
	// authorized_keys del disco base, por eso usamos contraseña)
	err = runSSHCommandWithPassword(port, "root", rootPassword, remoteCommand)
	if err != nil {
		return fmt.Errorf("error al crear usuario: %v", err)
	}

	// Verificar conexión con el nuevo usuario usando su llave
	err = runSSHCommandWithKey(port, username, privateKeyPath, "echo OK")
	if err != nil {
		return fmt.Errorf("verificación de llaves del usuario falló: %v", err)
	}

	if !wasRunning {
		s.VBoxSvc.StopVM(vmName)
	}

	return nil
}

// GetPrivateKeyPath returns the path to a private key
func (s *SSHService) GetPrivateKeyPath(vmName string, username string) string {
	return filepath.Join(s.KeysDir, vmName, fmt.Sprintf("id_rsa_%s", username))
}

// GetPublicKeyPath returns the path to a public key
func (s *SSHService) GetPublicKeyPath(vmName string, username string) string {
	return filepath.Join(s.KeysDir, vmName, fmt.Sprintf("id_rsa_%s.pub", username))
}

// KeysExist checks if keys exist for a VM and username
func (s *SSHService) KeysExist(vmName string, username string) bool {
	privPath := s.GetPrivateKeyPath(vmName, username)
	_, err := os.Stat(privPath)
	return err == nil
}
