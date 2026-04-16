#!/bin/bash
# build-sample-app.sh
# Compiles the sample application for Linux and creates a .zip file ready for deployment

set -e

echo "═══════════════════════════════════════════════════"
echo "  Compilando aplicación de ejemplo para Linux..."
echo "═══════════════════════════════════════════════════"

cd "$(dirname "$0")/sample-app"

# Compile for Linux AMD64 (typical VM target)
GOOS=linux GOARCH=amd64 go build -o sample-app .

echo "✓ Compilación exitosa: sample-app (linux/amd64)"

# Create zip
cd ..
mkdir -p dist
cp sample-app/sample-app dist/
cd dist
zip -j sample-app.zip sample-app
rm sample-app

echo "✓ Archivo zip creado: dist/sample-app.zip"
echo ""
echo "Para desplegar:"
echo "  1. Selecciona una VM en el dashboard"
echo "  2. Sube el archivo dist/sample-app.zip"
echo "  3. Comando ejecutable: sample-app"
echo "  4. Carpeta destino: /home/<usuario>/app (o la que prefieras)"
echo "═══════════════════════════════════════════════════"
