#!/usr/bin/env bash
# Builds the converter (uwp2xbox.exe), the registrar (azimuth-dlc.exe), the mod
# (dinput8.dll) and the installer (dist\FH3-Expansions-Converter-<version>-setup.exe).
# Needs Go, mingw-w64 (x86_64-w64-mingw32-gcc, windres) and Inno Setup 6. ISCC names
# the Inno Setup compiler when it is not in its usual per-user place.
# Every program carries Windows version information (winres/): builds without it were
# flagged by Defender's machine-learning scan as a false positive. Versions are in
# winres/*.rc and installer/fh3-expansions-convert.iss.
set -euo pipefail
cd "$(dirname "$0")"
WINDRES="${WINDRES:-windres}"
(cd winres && "$WINDRES" uwp2xbox.rc -O coff -o ../converter/rsrc_windows_amd64.syso \
           && "$WINDRES" azimuth-dlc.rc -O coff -o ../azimuth-dlc/rsrc_windows_amd64.syso)
(cd converter && go build -trimpath -o uwp2xbox.exe .)
(cd azimuth-dlc && go build -trimpath -o azimuth-dlc.exe .)
bash mod/build.sh
ISCC="${ISCC:-$LOCALAPPDATA/Programs/Inno Setup 6/ISCC.exe}"
"$ISCC" installer/fh3-expansions-convert.iss
