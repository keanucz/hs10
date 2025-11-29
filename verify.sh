#!/bin/bash

echo "🔍 Verifying ReplyChat setup..."
echo ""

errors=0

echo "✓ Checking Go version..."
if command -v go &> /dev/null; then
    GO_VERSION=$(go version)
    echo "  $GO_VERSION"
else
    echo "  ✗ Go not found"
    errors=$((errors + 1))
fi
echo ""

echo "✓ Checking project structure..."
required_dirs=("src" "src/template" "src/static" "src/agents" "data")
for dir in "${required_dirs[@]}"; do
    if [ -d "$dir" ]; then
        echo "  ✓ $dir/"
    else
        echo "  ✗ $dir/ missing"
        errors=$((errors + 1))
    fi
done
echo ""

echo "✓ Checking required files..."
required_files=(
    "src/main.go"
    "src/agents/processor.go"
    "src/template/index.html"
    "src/template/project.html"
    "src/static/styles.css"
    "src/static/app.js"
    "go.mod"
    "go.sum"
    "Dockerfile"
    "docker-compose.yml"
    ".env.example"
    ".gitignore"
    "README.md"
)
for file in "${required_files[@]}"; do
    if [ -f "$file" ]; then
        echo "  ✓ $file"
    else
        echo "  ✗ $file missing"
        errors=$((errors + 1))
    fi
done
echo ""

echo "✓ Checking dependencies..."
if go list -m all &> /dev/null; then
    echo "  ✓ Dependencies OK"
    go list -m github.com/gorilla/websocket
    go list -m github.com/google/uuid
    go list -m github.com/glebarez/go-sqlite
    go list -m github.com/joho/godotenv
else
    echo "  ✗ Dependencies not installed"
    echo "  Run: go mod download"
    errors=$((errors + 1))
fi
echo ""

echo "✓ Attempting to build..."
if go build -o replychat-test ./src 2>&1; then
    echo "  ✓ Build successful"
    rm -f replychat-test
else
    echo "  ✗ Build failed"
    errors=$((errors + 1))
fi
echo ""

echo "✓ Checking environment setup..."
if [ -f ".env" ]; then
    echo "  ✓ .env file exists"
else
    echo "  ℹ .env file not found (optional)"
    echo "    Copy .env.example to .env for local config"
fi
echo ""

echo "✓ Checking Docker..."
if command -v docker &> /dev/null; then
    echo "  ✓ Docker installed"
    DOCKER_VERSION=$(docker --version)
    echo "    $DOCKER_VERSION"
else
    echo "  ℹ Docker not found (optional)"
fi
echo ""

echo "=========================================="
if [ $errors -eq 0 ]; then
    echo "✅ Verification complete! No errors found."
    echo ""
    echo "Next steps:"
    echo "  1. Copy .env.example to .env (if needed)"
    echo "  2. Run: make dev"
    echo "  3. Open: http://localhost:8080"
else
    echo "❌ Verification found $errors error(s)"
    echo "Please fix the errors above before running."
fi
echo "=========================================="
