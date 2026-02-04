#!/bin/bash
set -e

# Release script for lingti-bot
# Usage: ./scripts/release.sh <version>
# Example: ./scripts/release.sh 1.2.0

VERSION=$1
PROJECTNAME="lingti-bot"

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 1.2.0"
    exit 1
fi

# Validate version format (semver)
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Version must be in semver format (e.g., 1.2.0)"
    exit 1
fi

echo "==> Building release v$VERSION"

# Clean previous builds
rm -rf dist
mkdir -p dist

# Update version in Makefile
sed -i '' "s/^VERSION := .*/VERSION := $VERSION/" Makefile

# Build all platforms
echo "==> Building darwin-amd64..."
make darwin-amd64

echo "==> Building darwin-arm64..."
make darwin-arm64

echo "==> Building linux-amd64..."
make linux-amd64

echo "==> Building linux-arm64..."
make linux-arm64

echo "==> Building windows-amd64..."
make windows-amd64

echo "==> Building windows-arm64..."
make windows-arm64

# Create archives
echo "==> Creating archives..."
cd dist

# macOS
tar -czf "${PROJECTNAME}-${VERSION}-darwin-amd64.tar.gz" "${PROJECTNAME}-${VERSION}-darwin-amd64"
tar -czf "${PROJECTNAME}-${VERSION}-darwin-arm64.tar.gz" "${PROJECTNAME}-${VERSION}-darwin-arm64"

# Linux
tar -czf "${PROJECTNAME}-${VERSION}-linux-amd64.tar.gz" "${PROJECTNAME}-${VERSION}-linux-amd64"
tar -czf "${PROJECTNAME}-${VERSION}-linux-arm64.tar.gz" "${PROJECTNAME}-${VERSION}-linux-arm64"

# Windows
zip -q "${PROJECTNAME}-${VERSION}-windows-amd64.zip" "${PROJECTNAME}-${VERSION}-windows-amd64.exe"
zip -q "${PROJECTNAME}-${VERSION}-windows-arm64.zip" "${PROJECTNAME}-${VERSION}-windows-arm64.exe"

# Generate checksums
echo "==> Generating checksums..."
shasum -a 256 *.tar.gz *.zip > checksums.txt

cd ..

# Commit version bump
git add Makefile
git commit -m "chore: bump version to $VERSION" || true

# Create git tag
echo "==> Creating git tag v$VERSION..."
git tag -a "v$VERSION" -m "Release v$VERSION"

# Push tag
echo "==> Pushing tag to remote..."
git push origin "v$VERSION"

# Create GitHub release
echo "==> Creating GitHub release..."
gh release create "v$VERSION" \
    --title "v$VERSION" \
    --generate-notes \
    dist/*.tar.gz \
    dist/*.zip \
    dist/checksums.txt

echo "==> Release v$VERSION complete!"
echo "View at: https://github.com/ruilisi/lingti-bot/releases/tag/v$VERSION"
