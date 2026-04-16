# VM Platform — Parcial 2 (Parte 1 & Parte 2)

Plataforma web desarrollada en **Go (Golang)** para automatizar la gestión de máquinas virtuales en Oracle VirtualBox, despliegue de aplicaciones y gestión de servicios systemd mediante SSH.

---

## Requisitos Previos

- **Go** 1.21 o superior → [https://go.dev/dl/](https://go.dev/dl/)
- **Oracle VirtualBox** con `VBoxManage` accesible en el PATH
- Al menos **2 máquinas virtuales** con distribuciones Debian configuradas en VirtualBox
- **Servidor SSH** instalado y configurado en cada VM
- Las VMs deben tener acceso SSH con llaves de autenticación configuradas

---

## Estructura del Proyecto

```
vm-platform/
├── main.go                         # Punto de entrada del servidor web
├── go.mod / go.sum                 # Módulo de Go
├── build-sample-app.sh             # Script para compilar la app de ejemplo
├── handlers/
│   ├── handlers.go                 # Controladores HTTP (Parte 1 - VMs)
│   └── deploy_handlers.go          # Controladores HTTP (Parte 2 - Deploy/Services)
├── models/
│   └── models.go                   # Modelos de datos (Parte 1 + 2)
├── services/
│   ├── vbox_service.go             # Servicio VBoxManage
│   ├── ssh_service.go              # Servicio SSH (llaves RSA-1024)
│   ├── platform_service.go         # Servicio principal (Parte 1)
│   └── deploy_service.go           # Servicio de despliegue (Parte 2)
├── sample-app/
│   ├── main.go                     # Aplicación de ejemplo para gestionar
│   └── go.mod                      # Módulo de la app de ejemplo
├── templates/
│   └── index.html                  # Dashboard web completo (Parte 1 + 2)
└── static/
    └── css/ js/                    # Archivos estáticos
```

---

## Instalación y Ejecución

```bash
# 1. Clonar o copiar el proyecto
cd vm-platform

# 2. Compilar la plataforma
go build -o vm-platform .

# 3. Ejecutar
./vm-platform
```

El servidor se inicia en **http://localhost:8080**

---

## Compilar la Aplicación de Ejemplo

La aplicación de ejemplo escribe líneas incrementales con marca de tiempo a un archivo.

```bash
# Compilar para Linux (target de las VMs)
chmod +x build-sample-app.sh
./build-sample-app.sh

# El archivo zip se genera en: dist/sample-app.zip
```

**Formato de salida de la aplicación:**
```
1 - 2026-04-13 10:00:01
2 - 2026-04-13 10:01:15
3 - 2026-04-13 10:01:20
```

---

## Parte 1 — Gestión de Máquinas Virtuales

### Funcionalidades
- **Agregar VM Base**: Registra una VM existente de VirtualBox
- **Crear Llaves Root**: Genera par RSA-1024, despliega vía SSH
- **Crear Disco Multiconexión**: Convierte disco a tipo multiattach
- **Crear VM de Usuario**: Nueva VM compartiendo disco base
- **Crear Usuario**: Cuenta SSH con llaves RSA en la VM

### API REST (Parte 1)

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/api/dashboard` | Datos del dashboard |
| GET | `/api/vms/available` | VMs disponibles en VirtualBox |
| POST | `/api/base-vm/add` | Agregar VM base |
| POST | `/api/base-vm/create-keys` | Crear llaves SSH de root |
| GET | `/api/base-vm/download-keys` | Descargar llave privada root |
| POST | `/api/disk/create` | Crear disco multiconexión |
| POST | `/api/disk/disconnect` | Desconectar disco |
| POST | `/api/disk/connect` | Conectar disco |
| POST | `/api/disk/delete` | Eliminar disco |
| POST | `/api/user-vm/create` | Crear VM de usuario |
| POST | `/api/user-vm/create-user` | Crear usuario en VM |
| GET | `/api/user-vm/download-keys` | Descargar llaves de usuario |
| POST | `/api/user-vm/delete` | Eliminar VM de usuario |

---

## Parte 2 — Despliegue y Gestión de Servicios systemd

### Flujo de Trabajo

1. **Seleccionar VM**: Elegir una VM con usuario SSH configurado
2. **Subir archivo .zip**: Contiene la aplicación a ejecutar
3. **Configurar despliegue**: Carpeta destino, comando ejecutable, parámetros
4. **Desplegar**: La plataforma sube el zip, lo extrae, y prueba la ejecución
5. **Crear servicio systemd**: Se genera el archivo `.service` con `Restart=always` y `RestartSec=2`
6. **Gestionar servicio**: Iniciar, detener, reiniciar, habilitar/deshabilitar al arranque
7. **Monitorear logs**: Visualización en tiempo real del archivo de log (tail -f)

### Dashboard de Servicios

El dashboard incluye:
- **Indicadores de estado**: Muestra si el servicio está activo y habilitado al arranque
- **Botones de control**: Iniciar, Reiniciar, Detener, Habilitar, Deshabilitar
- **Estado del servicio**: Salida de `systemctl status`
- **Visor de logs en vivo**: Stream del contenido del archivo de log con tail -f

Los controles se habilitan/deshabilitan automáticamente según el estado del servicio.

### Configuración del Servicio systemd

El archivo de servicio generado tiene la siguiente estructura:

```ini
[Unit]
Description=Servicio gestionado - <nombre>
After=network.target

[Service]
Type=simple
ExecStart=<carpeta>/<ejecutable> <parametros>
WorkingDirectory=<carpeta>
User=<usuario>
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
```

- **Restart=always**: Se reinicia automáticamente si se detiene
- **RestartSec=2**: Verificación cada 2 segundos

### API REST (Parte 2)

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/api/deploy/vms` | VMs disponibles para despliegue |
| POST | `/api/deploy/upload` | Subir .zip y desplegar (multipart/form-data) |
| GET | `/api/deploy/list` | Lista de despliegues realizados |
| POST | `/api/service/create` | Crear servicio systemd |
| POST | `/api/service/action` | Acción del servicio (start/stop/restart/enable/disable) |
| GET | `/api/service/status` | Estado actual del servicio |
| GET | `/api/service/logs` | Contenido del archivo de log |
| GET | `/api/service/stream` | Stream SSE de tail -f (Server-Sent Events) |

---

## Especificaciones Técnicas

- **Algoritmo de llaves**: RSA con 1024 bits
- **Formato de llaves**: OpenSSH
- **Tipo de disco**: Multiattach (VirtualBox)
- **Red**: NAT con port forwarding para SSH
- **SSH nativo**: Librería golang.org/x/crypto/ssh (sin sshpass)
- **Persistencia**: Estado en archivos JSON (~/.vm-platform/)
- **Log streaming**: Server-Sent Events (SSE) para tail -f
- **Servicio systemd**: Restart=always, RestartSec=2

---

## Problemas Encontrados y Propuestas de Mejora

### Problemas
1. El tiempo de espera al iniciar VMs puede variar según los recursos del host
2. Los discos multiattach de VirtualBox requieren una secuencia exacta de desconexión/conversión
3. La transferencia de archivos grandes por SSH pipe puede ser lenta

### Propuestas de Mejora
1. Usar SFTP en lugar de pipe SSH para transferencia de archivos grandes
2. WebSocket en lugar de SSE para comunicación bidireccional
3. Soporte para múltiples servicios por VM
4. Editor web del archivo de configuración del daemon
5. Métricas de CPU/RAM del servicio en el dashboard
6. Autenticación web con usuario/contraseña

---

## Conocimientos Aprendidos

- Gestión programática de VirtualBox mediante VBoxManage
- Generación de pares de llaves RSA en Go con golang.org/x/crypto/ssh
- Despliegue automatizado de llaves SSH sin sshpass
- Creación y gestión de servicios systemd programáticamente
- Arquitectura de servicios REST en Go
- Server-Sent Events (SSE) para streaming en tiempo real
- Transferencia de archivos sobre SSH en Go
- Discos multiattach y copy-on-write en VirtualBox
