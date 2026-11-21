; Installer that converts the PC (UWP) copies of the Forza Horizon 3 expansions,
; Hot Wheels and Blizzard Mountain, for Azimuth. It carries no game file and no
; Microsoft file: only uwp2xbox.exe (the converter), azimuth-dlc.exe (writes the
; package list and content= for Azimuth) and dinput8.dll (the fh3-expansions mod,
; mod\). It asks for the Azimuth folder and for the PC copies of
; the base game and of both expansions, converts them into the install folder,
; puts dinput8.dll next to azimuth.exe, writes fh3-expansions.ini there and sets
; content= in data\azimuth.ini. azimuth.exe itself is left unchanged (put back
; to the original if version 1.0.0 of this setup had patched it).
; Uninstalling reverts Azimuth and removes the converted files.
; Built with Inno Setup (Jordan Russell and Martijn Laan, jrsoftware.org).
;
; Build: build.sh in the repository root (Go, mingw-w64, Inno Setup 6).
; Silent: setup.exe /VERYSILENT /AZIMUTH=<folder> /DUMP=<folder> /DIR=<folder> (/UWP=, /HOTWHEELS=, /BLIZZARD= name single packages)

#ifndef ConverterExe
  #define ConverterExe "..\converter\uwp2xbox.exe"
#endif
#ifndef PatcherExe
  #define PatcherExe "..\azimuth-dlc\azimuth-dlc.exe"
#endif
#ifndef ModDll
  #define ModDll "..\mod\dinput8.dll"
#endif
#ifndef OutDir
  #define OutDir "..\dist"
#endif
#ifndef AppVersion
  #define AppVersion "1.1.2"
#endif

[Setup]
AppId={{B7D0A6C4-3E1F-4C52-9B0E-0F3D1C000002}
AppName=FH3 Expansions Converter for Azimuth
AppVersion={#AppVersion}
AppPublisher=FH3MP
DefaultDirName={sd}\Forza\FH3 Expansions
DisableProgramGroupPage=yes
DisableReadyPage=no
OutputDir={#OutDir}
OutputBaseFilename=FH3-Expansions-Converter-{#AppVersion}-setup
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
WizardStyle=modern
Uninstallable=yes
UninstallDisplayName=FH3 Expansions Converter for Azimuth
#ifdef Sign
; sign.ps1 defines Sign and the sign tool "azsign" (Azure Artifact Signing)
SignTool=azsign
SignedUninstaller=yes
#endif

[Files]
Source: "{#ConverterExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PatcherExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#ModDll}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#ConverterExe}"; Flags: dontcopy
Source: "{#PatcherExe}"; Flags: dontcopy

[UninstallDelete]
Type: filesandordirs; Name: "{app}\HotWheels"
Type: filesandordirs; Name: "{app}\BlizzardMountain"
Type: files; Name: "{app}\fh3-expansions-conversion.txt"
Type: files; Name: "{app}\azimuth-folder.txt"

[Messages]
WizardSelectDir=Converted expansions
SelectDirDesc=Where should the converted expansions go?
SelectDirLabel3=Hot Wheels and Blizzard Mountain are converted for Azimuth into this folder, about 9 GB. Azimuth loads them from here, so the folder stays.
FinishedHeadingLabel=Hot Wheels and Blizzard Mountain are ready
FinishedLabelNoIcons=Start Azimuth and pick Forza Horizon 3. Both expansions are reached through their travel points on the map.
FinishedLabel=Start Azimuth and pick Forza Horizon 3. Both expansions are reached through their travel points on the map.

[Code]
var
  AzimuthPage, DumpPage: TInputDirWizardPage;
  DirSuggested, Found: String;

function Folder(Page: TInputDirWizardPage; Index: Integer): String;
begin
  Result := RemoveBackslashUnlessRoot(Trim(Page.Values[Index]));
end;

// Runs a program hidden; Msg gets what it wrote to stderr, FoundOut the lines of
// stdout that start with "found " (the packages it found).
function RunTool(const Exe, Params: String; var Msg, FoundOut: String): Boolean;
var
  Output: TExecOutput;
  ResultCode, I: Integer;
  Line: String;
begin
  Msg := '';
  FoundOut := '';
  ResultCode := -1;
  Result := ExecAndCaptureOutput(Exe, Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode, Output) and (ResultCode = 0);
  for I := 0 to GetArrayLength(Output.StdErr) - 1 do
  begin
    if Msg <> '' then
      Msg := Msg + #13#10;
    Msg := Msg + Output.StdErr[I];
  end;
  for I := 0 to GetArrayLength(Output.StdOut) - 1 do
  begin
    Line := Output.StdOut[I];
    if Copy(Line, 1, 6) = 'found ' then
    begin
      Delete(Line, 1, 6);
      Line := Uppercase(Copy(Line, 1, 1)) + Copy(Line, 2, Length(Line));
      if FoundOut <> '' then
        FoundOut := FoundOut + #13#10;
      FoundOut := FoundOut + Line;
    end;
  end;
  if Copy(Msg, 1, 7) = 'error: ' then
    Delete(Msg, 1, 7);
  if Msg <> '' then
    Msg := Uppercase(Copy(Msg, 1, 1)) + Copy(Msg, 2, Length(Msg));
  if (not Result) and (Msg = '') then
    Msg := 'The converter did not run.';
end;

// A path as one command-line argument. A trailing backslash (a drive root such
// as S:\) is doubled, or it would escape the closing quote.
function Arg(const Path: String): String;
begin
  Result := Path;
  if (Length(Result) > 0) and (Result[Length(Result)] = '\') then
    Result := Result + '\';
  Result := '"' + Result + '"';
end;

function SetupArgs(const OutDir: String): String;
begin
  Result := '-azimuth ' + Arg(Folder(AzimuthPage, 0)) +
    ' -dump ' + Arg(Folder(DumpPage, 0)) +
    ' -out ' + Arg(RemoveBackslashUnlessRoot(OutDir));
  // single packages can still be named on the command line
  if ExpandConstant('{param:UWP|}') <> '' then
    Result := Result + ' -uwp ' + Arg(ExpandConstant('{param:UWP|}'));
  if ExpandConstant('{param:HOTWHEELS|}') <> '' then
    Result := Result + ' -hotwheels ' + Arg(ExpandConstant('{param:HOTWHEELS|}'));
  if ExpandConstant('{param:BLIZZARD|}') <> '' then
    Result := Result + ' -blizzard ' + Arg(ExpandConstant('{param:BLIZZARD|}'));
end;

procedure InitializeWizard;
begin
  AzimuthPage := CreateInputDirPage(wpWelcome, 'Azimuth', 'Where is Azimuth?',
    'The folder with azimuth.exe. Forza Horizon 3 has to be added in its launcher: the converter takes the console shaders from that copy of the game.',
    False, '');
  AzimuthPage.Add('');
  AzimuthPage.Values[0] := ExpandConstant('{param:AZIMUTH|}');

  DumpPage := CreateInputDirPage(AzimuthPage.ID, 'Forza Horizon 3 for Windows', 'Where is your Windows copy?',
    'The folder of the Windows base game, or one that holds it. Hot Wheels and Blizzard Mountain are looked for there first, then on the rest of that drive. Nothing in them is changed.',
    False, '');
  DumpPage.Add('');
  DumpPage.Values[0] := ExpandConstant('{param:DUMP|}');
  if DumpPage.Values[0] = '' then
    DumpPage.Values[0] := ExpandConstant('{param:UWP|}');
  DirSuggested := '';
  Found := '';
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  Msg, Suggest: String;
  Progress: TOutputProgressWizardPage;
begin
  Result := True;
  if CurPageID = AzimuthPage.ID then
  begin
    if not FileExists(AddBackslash(Folder(AzimuthPage, 0)) + 'azimuth.exe') then
    begin
      SuppressibleMsgBox('No azimuth.exe in that folder.', mbError, MB_OK, IDOK);
      Result := False;
      exit;
    end;
    // the converted files go next to Azimuth unless another folder was chosen
    // (in the wizard, by an earlier install, or with /DIR=)
    if (WizardForm.PrevAppDir = '') and (ExpandConstant('{param:DIR|}') = '') and
       ((DirSuggested = '') or (WizardForm.DirEdit.Text = DirSuggested)) then
    begin
      Suggest := AddBackslash(ExtractFileDir(Folder(AzimuthPage, 0))) + 'FH3 Expansions';
      WizardForm.DirEdit.Text := Suggest;
      DirSuggested := Suggest;
    end;
  end
  else if CurPageID = DumpPage.ID then
  begin
    ExtractTemporaryFile('uwp2xbox.exe');
    ExtractTemporaryFile('azimuth-dlc.exe');
    Progress := CreateOutputProgressPage('Forza Horizon 3 for Windows', 'Looking for the base game and both expansions');
    Progress.SetText('Looking through ' + Folder(DumpPage, 0) + ' and, if needed, the rest of its drive ...', 'This can take up to a minute.');
    Progress.Show;
    try
      if not RunTool(ExpandConstant('{tmp}\uwp2xbox.exe'),
           'setup -check ' + SetupArgs(ExpandConstant('{tmp}\check')) + ' -tool "' + ExpandConstant('{tmp}\azimuth-dlc.exe') + '"', Msg, Found) then
      begin
        SuppressibleMsgBox(Msg, mbError, MB_OK, IDOK);
        Result := False;
      end;
    finally
      Progress.Hide;
    end;
  end;
end;

function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo, MemoTypeInfo, MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: String): String;
var
  F: String;
begin
  F := Found;
  StringChangeEx(F, #13#10, NewLine + Space, True);
  Result := 'Azimuth:' + NewLine + Space + Folder(AzimuthPage, 0) + NewLine + NewLine +
    'Windows copy:' + NewLine + Space + F + NewLine + NewLine +
    'Converted expansions:' + NewLine + Space + WizardDirValue + NewLine + NewLine +
    'What happens:' + NewLine +
    Space + 'Both expansions are converted for the Xbox One game: about 5 minutes and up to 9 GB of memory.' + NewLine +
    Space + 'Azimuth gets the expansions mod (dinput8.dll) and its list (fh3-expansions.ini). azimuth.exe is not changed.' + NewLine +
    Space + 'Uninstalling removes both and the converted files.';
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
  Pause: String;
begin
  if CurStep <> ssPostInstall then
    exit;
  SaveStringToFile(ExpandConstant('{app}\azimuth-folder.txt'), Folder(AzimuthPage, 0), False);
  // the converter writes its progress into a window of its own
  Pause := '-pause ';
  if WizardSilent then
    Pause := '';
  if (not Exec(ExpandConstant('{app}\uwp2xbox.exe'), 'setup ' + Pause + SetupArgs(ExpandConstant('{app}')), '', SW_SHOW, ewWaitUntilTerminated, ResultCode)) or (ResultCode <> 0) then
    SuppressibleMsgBox('The conversion did not finish. Azimuth was not changed. Run this setup again.', mbError, MB_OK, IDOK);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Saved: AnsiString;
  ResultCode: Integer;
begin
  if CurUninstallStep <> usUninstall then
    exit;
  if LoadStringFromFile(ExpandConstant('{app}\azimuth-folder.txt'), Saved) and (Trim(String(Saved)) <> '') then
    Exec(ExpandConstant('{app}\azimuth-dlc.exe'), '-azimuth ' + Arg(Trim(String(Saved))) + ' -revert', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;
