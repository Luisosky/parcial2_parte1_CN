package models

import "time"

// BaseVM represents a base virtual machine in VirtualBox
type BaseVM struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	KeysCreated bool      `json:"keys_created"`
	DiskCreated bool      `json:"disk_created"`
	CreatedAt   time.Time `json:"created_at"`
}

// MultiAttachDisk represents a multi-attach disk created from a base VM
type MultiAttachDisk struct {
	Name       string    `json:"name"`
	BaseVMName string    `json:"base_vm_name"`
	DiskPath   string    `json:"disk_path"`
	Connected  bool      `json:"connected"`
	CreatedAt  time.Time `json:"created_at"`
}

// UserVM represents a user virtual machine created from a multi-attach disk
type UserVM struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DiskName    string    `json:"disk_name"`
	BaseVMName  string    `json:"base_vm_name"`
	UserCreated bool      `json:"user_created"`
	Username    string    `json:"username"`
	Running     bool      `json:"running"`
	CreatedAt   time.Time `json:"created_at"`
}

// SSHKeyInfo stores info about generated SSH keys
type SSHKeyInfo struct {
	VMName     string `json:"vm_name"`
	Username   string `json:"username"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	KeyPath    string `json:"key_path"`
}

// ─── Parte 2: Deployment & Service Management ───

// DeploymentInfo represents a deployed application on a VM
type DeploymentInfo struct {
	VMName         string    `json:"vm_name"`
	Username       string    `json:"username"`
	Password       string    `json:"password,omitempty"`
	RootPassword   string    `json:"root_password,omitempty"`
	ZipFileName    string    `json:"zip_file_name"`
	DestFolder     string    `json:"dest_folder"`
	ExecCommand    string    `json:"exec_command"`
	ExecParams     string    `json:"exec_params"`
	ServiceName    string    `json:"service_name"`
	ServiceFile    string    `json:"service_file"`
	LogFile        string    `json:"log_file"`
	Deployed       bool      `json:"deployed"`
	ServiceCreated bool      `json:"service_created"`
	CreatedAt      time.Time `json:"created_at"`
}

// DeployRequest is the request to deploy an application
type DeployRequest struct {
	VMName      string `json:"vm_name"`
	Username    string `json:"username"`
	DestFolder  string `json:"dest_folder"`
	ExecCommand string `json:"exec_command"`
	ExecParams  string `json:"exec_params"`
}

// ServiceCreateRequest is the request to create a systemd service
type ServiceCreateRequest struct {
	VMName       string `json:"vm_name"`
	ServiceName  string `json:"service_name"`
	RootPassword string `json:"root_password"`
}

// ServiceActionRequest is the request for service actions
type ServiceActionRequest struct {
	VMName      string `json:"vm_name"`
	ServiceName string `json:"service_name"`
}

// ServiceStatus represents the current status of a systemd service
type ServiceStatus struct {
	VMName      string `json:"vm_name"`
	ServiceName string `json:"service_name"`
	Active      string `json:"active"`
	Enabled     string `json:"enabled"`
	StatusText  string `json:"status_text"`
	Running     bool   `json:"running"`
	IsEnabled   bool   `json:"is_enabled"`
}

// APIResponse is a generic API response
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// CreateBaseVMRequest is the request to create a base VM
type CreateBaseVMRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateUserVMRequest is the request to create a user VM
type CreateUserVMRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DiskName    string `json:"disk_name"`
}

// CreateUserRequest is the request to create a user in a VM
type CreateUserRequest struct {
	VMName       string `json:"vm_name"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	RootPassword string `json:"root_password"`
}
