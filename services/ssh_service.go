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

type SSHService struct {
	KeysDir string
	VBoxSvc *VBoxService
}

func NewSSHService(vboxSvc *VBoxService) *SSHService {
	usr, _ := user.Current()
	keysDir := filepath.Join(usr.HomeDir, ".vm-platform", "keys")
	os.MkdirAll(keysDir, 0700)
	return &SSHService{KeysDir: keysDir, VBoxSvc: vboxSvc}
}

func (s *SSHService) GenerateRSAKeyPair(vmName string, username string) (privateKeyPath string, publicKeyPath string, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", fmt.Errorf("error al generar llave RSA: %v", err)
	}

	vmKeysDir := filepath.Join(s.KeysDir, vmName)
	os.MkdirAll(vmKeysDir, 0700)

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

func runSSHCommandWithPassword(port string, username string, password string, command string) error {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
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
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
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

	err = runSSHCommandWithPassword(port, "root", rootPassword, remoteCommand)
	if err != nil {
		return fmt.Errorf("error al desplegar llaves de root: %v", err)
	}

	err = runSSHCommandWithKey(port, "root", privateKeyPath, "echo OK")
	if err != nil {
		return fmt.Errorf("verificación de llaves falló: %v", err)
	}

	if !wasRunning {
		s.VBoxSvc.StopVM(vmName)
	}

	return nil
}

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

	err = runSSHCommandWithPassword(port, "root", rootPassword, remoteCommand)
	if err != nil {
		return fmt.Errorf("error al crear usuario: %v", err)
	}

	err = runSSHCommandWithKey(port, username, privateKeyPath, "echo OK")
	if err != nil {
		return fmt.Errorf("verificación de llaves del usuario falló: %v", err)
	}

	if !wasRunning {
		s.VBoxSvc.StopVM(vmName)
	}

	return nil
}

func (s *SSHService) GetPrivateKeyPath(vmName string, username string) string {
	return filepath.Join(s.KeysDir, vmName, fmt.Sprintf("id_rsa_%s", username))
}

func (s *SSHService) GetPublicKeyPath(vmName string, username string) string {
	return filepath.Join(s.KeysDir, vmName, fmt.Sprintf("id_rsa_%s.pub", username))
}

func (s *SSHService) KeysExist(vmName string, username string) bool {
	privPath := s.GetPrivateKeyPath(vmName, username)
	_, err := os.Stat(privPath)
	return err == nil
}

// runSSHCommandWithKeyOrPassword tries key auth first, falls back to password if provided
func runSSHCommandWithKeyOrPassword(port, username, keyPath, password, command string) error {
	var authMethods []ssh.AuthMethod

	// Try to load key
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

// RepairAllKeys regenerates and redeploys both root and user keys using root password auth.
// Use when local keys exist but don't match the VM's authorized_keys.
func (s *SSHService) RepairAllKeys(vmName, username, rootPassword string) error {
	state, _ := s.VBoxSvc.GetVMState(vmName)
	if state != "running" {
		if err := s.VBoxSvc.StartVM(vmName); err != nil {
			return fmt.Errorf("error al iniciar VM: %v", err)
		}
		time.Sleep(30 * time.Second)
	}

	port, err := s.VBoxSvc.GetNATPort(vmName)
	if err != nil || port == "" {
		return fmt.Errorf("error al obtener puerto SSH: %v", err)
	}

	// 1. Regenerate and deploy root keys
	_, rootPubPath, err := s.GenerateRSAKeyPair(vmName, "root")
	if err != nil {
		return fmt.Errorf("error al generar llaves root: %v", err)
	}
	rootPubData, err := os.ReadFile(rootPubPath)
	if err != nil {
		return fmt.Errorf("error al leer llave pública root: %v", err)
	}
	rootPubStr := strings.TrimSpace(string(rootPubData))
	rootCmd := fmt.Sprintf(`mkdir -p /root/.ssh && chmod 700 /root/.ssh && echo "%s" > /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys`, rootPubStr)
	if err := runSSHCommandWithPassword(port, "root", rootPassword, rootCmd); err != nil {
		return fmt.Errorf("error al instalar llave root (¿contraseña incorrecta?): %v", err)
	}

	// Verify root key works
	rootKeyPath := s.GetPrivateKeyPath(vmName, "root")
	if err := runSSHCommandWithKey(port, "root", rootKeyPath, "echo OK"); err != nil {
		return fmt.Errorf("verificación de llave root falló: %v", err)
	}

	// 2. Regenerate and deploy user keys using root (now working)
	if username == "" || username == "root" {
		return nil
	}
	_, userPubPath, err := s.GenerateRSAKeyPair(vmName, username)
	if err != nil {
		return fmt.Errorf("error al generar llaves de usuario: %v", err)
	}
	userPubData, err := os.ReadFile(userPubPath)
	if err != nil {
		return fmt.Errorf("error al leer llave pública de usuario: %v", err)
	}
	userPubStr := strings.TrimSpace(string(userPubData))
	userCmd := fmt.Sprintf(`mkdir -p /home/%s/.ssh && chmod 700 /home/%s/.ssh && echo "%s" > /home/%s/.ssh/authorized_keys && chmod 600 /home/%s/.ssh/authorized_keys && chown -R %s:%s /home/%s/.ssh`,
		username, username, userPubStr, username, username, username, username, username)
	if err := runSSHCommandWithKey(port, "root", rootKeyPath, userCmd); err != nil {
		return fmt.Errorf("error al instalar llave de usuario: %v", err)
	}

	// Verify user key works
	userKeyPath := s.GetPrivateKeyPath(vmName, username)
	if err := runSSHCommandWithKey(port, username, userKeyPath, "echo OK"); err != nil {
		return fmt.Errorf("verificación de llave de usuario falló: %v", err)
	}

	return nil
}
