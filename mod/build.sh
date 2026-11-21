#!/usr/bin/env bash
# Builds dinput8.dll (fh3-expansions) with mingw-w64, with the version information
# from ../winres/dinput8.rc.
set -euo pipefail
cd "$(dirname "$0")"
WINDRES="${WINDRES:-windres}"
(cd ../winres && "$WINDRES" dinput8.rc -O coff -o ../mod/version.o)
x86_64-w64-mingw32-gcc -shared -O2 -s -Wall -Wextra -static-libgcc -o dinput8.dll fh3exp.c version.o dinput8.def
rm -f version.o
