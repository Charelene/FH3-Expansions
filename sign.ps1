# Signs a release (Authenticode through Azure Artifact Signing), after build.sh:
# uwp2xbox.exe, azimuth-dlc.exe and dinput8.dll are signed in place, then the
# installer is compiled again so it packs the signed files and Inno Setup signs
# the setup and its uninstaller. Every file must then be Valid and timestamped.
#
#   powershell -ExecutionPolicy Bypass -File sign.ps1 -SigningDir <folder>
#
# <folder> (or FH3EXP_SIGNING_DIR) holds metadata.json (endpoint, account and
# certificate profile), signtool.exe from the Windows SDK package under sdk\bin
# and Azure.CodeSigning.Dlib.dll from the Trusted Signing client package under
# ts\bin\x64. The dlib signs with the maintainer's own az login.
param(
    [string]$SigningDir = $env:FH3EXP_SIGNING_DIR,
    [string]$Iscc = $(if ($env:ISCC) { $env:ISCC } else { Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe' })
)
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
if (-not $SigningDir) { throw 'give -SigningDir (or set FH3EXP_SIGNING_DIR)' }

$signtool = Get-ChildItem (Join-Path $SigningDir 'sdk\bin') -Recurse -Filter signtool.exe |
    Where-Object { $_.FullName -like '*\x64\signtool.exe' } | Sort-Object FullName | Select-Object -Last 1
$dlib = Join-Path $SigningDir 'ts\bin\x64\Azure.CodeSigning.Dlib.dll'
$metadata = Join-Path $SigningDir 'metadata.json'
if (-not $signtool -or -not (Test-Path $dlib) -or -not (Test-Path $metadata)) {
    throw "no signtool, Azure.CodeSigning.Dlib.dll or metadata.json under $SigningDir"
}
$signArgs = @('sign', '/q', '/fd', 'SHA256', '/tr', 'http://timestamp.acs.microsoft.com', '/td', 'SHA256', '/dlib', $dlib, '/dmdf', $metadata)

function Test-Signed([string[]]$files) {
    foreach ($f in $files) {
        $s = Get-AuthenticodeSignature $f
        if ($s.Status -ne 'Valid' -or -not $s.TimeStamperCertificate) { throw "$f is $($s.Status) after signing" }
        Write-Host "signed: $(Split-Path $f -Leaf) by $($s.SignerCertificate.Subject)"
    }
}

$bins = @('converter\uwp2xbox.exe', 'azimuth-dlc\azimuth-dlc.exe', 'mod\dinput8.dll') | ForEach-Object { (Resolve-Path $_).Path }
& $signtool.FullName @signArgs @bins
if ($LASTEXITCODE -ne 0) { throw "signtool failed ($LASTEXITCODE): is the az login current (az login)?" }
Test-Signed $bins

# Inno Setup runs the sign tool for the uninstaller and the setup ($f = the file, $q = a quote)
$tool = '$q' + $signtool.FullName + '$q sign /q /fd SHA256 /tr http://timestamp.acs.microsoft.com /td SHA256 /dlib $q' + $dlib + '$q /dmdf $q' + $metadata + '$q $f'
& $Iscc '/DSign' "/Sazsign=$tool" 'installer\fh3-expansions-convert.iss'
if ($LASTEXITCODE -ne 0) { throw "ISCC failed ($LASTEXITCODE)" }
Test-Signed (Get-ChildItem dist -Filter '*-setup.exe' | ForEach-Object FullName)
