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

// ─── Parte 3: Elasticidad Automática ───

// LoadBalancer represents an HAProxy load balancer running on a VM
type LoadBalancer struct {
	Name         string            `json:"name"`
	VMName       string            `json:"vm_name"`
	Username     string            `json:"username"`
	RootPassword string            `json:"root_password,omitempty"`
	FrontendPort int               `json:"frontend_port"`
	BackendPort  int               `json:"backend_port"`
	Algorithm    string            `json:"algorithm"`
	Backends     []*BackendServer  `json:"backends"`
	Installed    bool              `json:"installed"`
	Running      bool              `json:"running"`
	Elasticity   *ElasticityConfig `json:"elasticity,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// BackendServer represents a single backend application server registered in HAProxy
type BackendServer struct {
	Name       string    `json:"name"`
	VMName     string    `json:"vm_name"`
	Username   string    `json:"username"`
	Address    string    `json:"address"`
	Port       int       `json:"port"`
	Weight     int       `json:"weight"`
	Enabled    bool      `json:"enabled"`
	AutoScaled bool      `json:"auto_scaled"`
	DiskName   string    `json:"disk_name,omitempty"`
	AddedAt    time.Time `json:"added_at"`
}

// ElasticityConfig holds the thresholds and timing configuration for auto-scaling
type ElasticityConfig struct {
	Enabled           bool      `json:"enabled"`
	UpperThresholdCPU float64   `json:"upper_threshold_cpu"`
	LowerThresholdCPU float64   `json:"lower_threshold_cpu"`
	SustainedSeconds  int       `json:"sustained_seconds"`
	SampleIntervalSec int       `json:"sample_interval_sec"`
	MinBackends       int       `json:"min_backends"`
	MaxBackends       int       `json:"max_backends"`
	CooldownSeconds   int       `json:"cooldown_seconds"`
	BaseDiskName      string    `json:"base_disk_name"`
	NewVMNamePrefix   string    `json:"new_vm_name_prefix"`
	NewVMUser         string    `json:"new_vm_user"`
	AppExecCommand    string    `json:"app_exec_command"`
	AppPort           int       `json:"app_port"`
	LastScaleAction   time.Time `json:"last_scale_action,omitempty"`
	LastScaleType     string    `json:"last_scale_type,omitempty"`
}

// CPUMetric is a single point-in-time CPU measurement on a backend VM
type CPUMetric struct {
	VMName     string    `json:"vm_name"`
	CPUPercent float64   `json:"cpu_percent"`
	Timestamp  time.Time `json:"timestamp"`
	Error      string    `json:"error,omitempty"`
}

// MetricsSnapshot is the latest view of metrics plus elasticity state, for the UI
type MetricsSnapshot struct {
	LBName           string           `json:"lb_name"`
	Backends         []*BackendServer `json:"backends"`
	Metrics          []CPUMetric      `json:"metrics"`
	MaxCPUPercent    float64          `json:"max_cpu_percent"`
	AvgCPUPercent    float64          `json:"avg_cpu_percent"`
	CurrentBackends  int              `json:"current_backends"`
	HighSinceSeconds int              `json:"high_since_seconds"`
	LowSinceSeconds  int              `json:"low_since_seconds"`
	LastAction       string           `json:"last_action,omitempty"`
	LastActionTime   time.Time        `json:"last_action_time,omitempty"`
	Enabled          bool             `json:"enabled"`
	EventLog         []string         `json:"event_log,omitempty"`
}

// StressRequest requests stress-ng execution on a VM
type StressRequest struct {
	VMName      string `json:"vm_name"`
	CPUs        int    `json:"cpus"`
	LoadPercent int    `json:"load_percent"`
	Duration    int    `json:"duration_sec"`
}

// LoadBalancerCreateRequest to create a new LB
type LoadBalancerCreateRequest struct {
	Name         string `json:"name"`
	VMName       string `json:"vm_name"`
	Username     string `json:"username"`
	RootPassword string `json:"root_password"`
	FrontendPort int    `json:"frontend_port"`
	BackendPort  int    `json:"backend_port"`
	Algorithm    string `json:"algorithm"`
}

// BackendAddRequest adds a backend to an LB
type BackendAddRequest struct {
	LBName   string `json:"lb_name"`
	Name     string `json:"name"`
	VMName   string `json:"vm_name"`
	Username string `json:"username"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Weight   int    `json:"weight"`
}

// ElasticityUpdateRequest updates the elasticity config for an LB
type ElasticityUpdateRequest struct {
	LBName            string  `json:"lb_name"`
	Enabled           bool    `json:"enabled"`
	UpperThresholdCPU float64 `json:"upper_threshold_cpu"`
	LowerThresholdCPU float64 `json:"lower_threshold_cpu"`
	SustainedSeconds  int     `json:"sustained_seconds"`
	SampleIntervalSec int     `json:"sample_interval_sec"`
	MinBackends       int     `json:"min_backends"`
	MaxBackends       int     `json:"max_backends"`
	CooldownSeconds   int     `json:"cooldown_seconds"`
	BaseDiskName      string  `json:"base_disk_name"`
	NewVMNamePrefix   string  `json:"new_vm_name_prefix"`
	NewVMUser         string  `json:"new_vm_user"`
	AppExecCommand    string  `json:"app_exec_command"`
	AppPort           int     `json:"app_port"`
}
