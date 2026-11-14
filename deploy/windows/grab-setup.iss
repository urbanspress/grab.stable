#ifndef AppVersion
  #define AppVersion "1.1.0"
#endif

[Setup]
AppId={{7B6E6E9E-8F4A-4B8B-9B7A-3C2A8E4D1F10}
AppName=GRAB
AppVersion={#AppVersion}
AppVerName=GRAB {#AppVersion}
AppPublisher=Miles Development Experts LLC
AppPublisherURL=https://github.com/urbanspress/grab.stable
AppSupportURL=https://github.com/urbanspress/grab.stable/issues
AppUpdatesURL=https://github.com/urbanspress/grab.stable/releases
DefaultDirName={autopf}\GRAB
DisableDirPage=no
DisableProgramGroupPage=yes
DisableWelcomePage=no
DisableReadyPage=no
DisableFinishedPage=no
LicenseFile=..\..\LICENSE
OutputDir=..\..\dist
OutputBaseFilename=GRAB-Setup-{#AppVersion}
SetupIconFile=..\..\cmd\grab\grab.ico
UninstallDisplayIcon={app}\grab.exe
UninstallDisplayName=GRAB {#AppVersion}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
ChangesEnvironment=yes
VersionInfoVersion={#AppVersion}.0
VersionInfoCompany=Miles Development Experts LLC
VersionInfoDescription=GRAB Programming Language & Server Platform Installer
VersionInfoProductName=GRAB
VersionInfoProductVersion={#AppVersion}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "addtopath"; Description: "Add GRAB to the system PATH (recommended)"; GroupDescription: "System integration:"

[Files]
Source: "..\..\dist\grab-windows-amd64.exe"; DestDir: "{app}"; DestName: "grab.exe"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Code]
const
  EnvKey = 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment';

function NormalizeDir(const S: string): string;
begin
  Result := RemoveBackslashUnlessRoot(Trim(S));
end;

procedure AddDirToSystemPath(const DirToAdd: string);
var
  CurrentPath: string;
  TargetDir: string;
  Parts: TArrayOfString;
  I: Integer;
  NewPath: string;
  Found: Boolean;
begin
  TargetDir := NormalizeDir(DirToAdd);
  if TargetDir = '' then Exit;

  if not RegQueryStringValue(HKEY_LOCAL_MACHINE, EnvKey, 'Path', CurrentPath) then
    CurrentPath := '';

  Found := False;
  if CurrentPath <> '' then
  begin
    Parts := StringSplit(CurrentPath, [';'], stAll);
    for I := 0 to GetArrayLength(Parts) - 1 do
    begin
      if CompareText(NormalizeDir(Parts[I]), TargetDir) = 0 then
      begin
        Found := True;
        Break;
      end;
    end;
  end;

  if Found then Exit;

  NewPath := CurrentPath;
  while (Length(NewPath) > 0) and (NewPath[Length(NewPath)] = ';') do
    Delete(NewPath, Length(NewPath), 1);

  if NewPath = '' then
    NewPath := TargetDir
  else
    NewPath := NewPath + ';' + TargetDir;

  RegWriteExpandStringValue(HKEY_LOCAL_MACHINE, EnvKey, 'Path', NewPath);
end;

procedure RemoveDirFromSystemPath(const DirToRemove: string);
var
  CurrentPath: string;
  TargetDir: string;
  Parts: TArrayOfString;
  I: Integer;
  Part: string;
  NewPath: string;
begin
  TargetDir := NormalizeDir(DirToRemove);
  if TargetDir = '' then Exit;

  if not RegQueryStringValue(HKEY_LOCAL_MACHINE, EnvKey, 'Path', CurrentPath) then
    Exit;

  Parts := StringSplit(CurrentPath, [';'], stExcludeEmpty);
  NewPath := '';
  for I := 0 to GetArrayLength(Parts) - 1 do
  begin
    Part := Trim(Parts[I]);
    if (Part <> '') and (CompareText(NormalizeDir(Part), TargetDir) <> 0) then
    begin
      if NewPath = '' then
        NewPath := Part
      else
        NewPath := NewPath + ';' + Part;
    end;
  end;

  RegWriteExpandStringValue(HKEY_LOCAL_MACHINE, EnvKey, 'Path', NewPath);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('addtopath') then
    AddDirToSystemPath(ExpandConstant('{app}'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemoveDirFromSystemPath(ExpandConstant('{app}'));
end;
