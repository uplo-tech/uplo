#!/usr/bin/env bash
set -e

version="$1"

# Directory where the binaries produces by build-release.sh are stored.
binDir="$2"

# Directory of the Uplo-UI Repo.
uiDir="$3"
if [ -z "$version" ] || [ -z "$binDir" ] || [ -z "$uiDir" ]; then
  echo "Usage: $0 VERSION BIN_DIRECTORY UI_DIRECTORY"
  exit 1
fi

echo Version: "${version}"
echo Binaries Directory: "${binDir}"
echo UI Directory: "${uiDir}"
echo ""

if [ "$UPLO_SILENT_RELEASE" != "true" ]; then
	read -p "Continue (y/n)?" CONT
	if [ "$CONT" != "y" ]; then
		exit 1
	fi
fi
echo "Building Uplo-UI...";

# Get the absolute paths to avoid funny business with relative paths.
uiDir=$(realpath "${uiDir}")
binDir=$(realpath "${binDir}")

# Remove previously built UI binaries.
rm -r "${uiDir}"/release/

cd "${uiDir}"

# Copy over all the uploc/uplod binaries.
mkdir -p bin/{linux,mac,win}
cp "${binDir}"/Uplo-"${version}"-darwin-amd64/uploc bin/mac/
cp "${binDir}"/Uplo-"${version}"-darwin-amd64/uplod bin/mac/

cp "${binDir}"/Uplo-"${version}"-linux-amd64/uploc bin/linux/
cp "${binDir}"/Uplo-"${version}"-linux-amd64/uplod bin/linux/

cp "${binDir}"/Uplo-"${version}"-windows-amd64/uploc.exe bin/win/
cp "${binDir}"/Uplo-"${version}"-windows-amd64/uplod.exe bin/win/

# Build yarn deps.
yarn

# Build each of the UI binaries.
yarn package-linux
yarn package-win
yarn package

# Copy the UI binaries into the binDir. Also change the name at the same time.
# The UI builder doesn't handle 4 digit versions correctly and if the UI hasn't
# been updated the version might be stale.
for ext in AppImage dmg exe; do 
  mv "${uiDir}"/release/*."${ext}" "${binDir}"/Uplo-UI-"${version}"."${ext}"
	(
		cd "${binDir}"
		sha256sum Uplo-UI-"${version}"."${ext}" >> Uplo-"${version}"-SHA256SUMS.txt
	)
done
