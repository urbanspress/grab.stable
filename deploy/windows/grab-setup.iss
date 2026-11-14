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
WizardImageFile=wizard-welcome.bmp
WizardSmallImageFile=wizard-header.bmp
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

[Messages]
WelcomeLabel1=Welcome to the GRAB {#AppVersion} Setup Wizard
WelcomeLabel2=This will install GRAB {#AppVersion} on your computer.%n%nIt is recommended that you close all other applications before continuing.%n%nClick Next to continue, or Cancel to exit Setup.
WizardLicense=End-User License Agreement
LicenseLabel=Please read the following License Agreement carefully.
LicenseLabel3=Please read the following License Agreement. You must accept the terms of this agreement before continuing with the installation.
LicenseAccepted=I &accept the terms in the License Agreement
LicenseNotAccepted=I &do not accept the terms in the License Agreement
WizardSelectDir=Destination Folder
SelectDirDesc=Where should GRAB be installed?
SelectDirLabel3=Setup will install GRAB into the following folder.
SelectDirBrowseLabel=To continue, click Next. If you would like to select a different folder, click Browse.
WizardReady=Ready to Install GRAB
ReadyLabel1=Setup is now ready to begin installing GRAB {#AppVersion} on your computer.
ReadyLabel2a=Click Install to begin the installation, or click Back if you want to review or change any settings.
FinishedHeadingLabel=Completed the GRAB {#AppVersion} Setup Wizard
FinishedLabelNoIcons=Setup has finished installing GRAB {#AppVersion} on your computer. The GRAB CLI has been added to your system PATH and registered in Windows Installed Apps.
FinishedLabel=Setup has finished installing GRAB {#AppVersion} on your computer. The GRAB CLI has been added to your system PATH and registered in Windows Installed Apps.

[Files]
Source: "..\..\dist\grab-windows-amd64.exe"; DestDir: "{app}"; DestName: "grab.exe"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Code]
const
  EnvKey = 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment';
  BCM_SETSHIELD = $160C;

var
  Interrupted: Boolean;

function SendMessageW(hWnd: HWND; Msg: UINT; wParam: LongInt; lParam: LongInt): LongInt;
  external 'SendMessageW@user32.dll stdcall';

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

procedure InitializeWizard();
begin
  Interrupted := False;
end;

function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo, MemoTypeInfo,
  MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: string): string;
begin
  Result := '';
  if MemoDirInfo <> '' then
    Result := Result + MemoDirInfo + NewLine + NewLine;
  Result := Result + 'System integration:' + NewLine +
    Space + 'Add GRAB to the system PATH' + NewLine +
    Space + 'Register GRAB {#AppVersion} in Windows Installed Apps';
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  if Interrupted then
  begin
    Result := False;
    WizardForm.Close;
    Exit;
  end;
  Result := True;
end;

procedure CancelButtonClick(CurPageID: Integer; var Cancel, Confirm: Boolean);
begin
  if Interrupted or (CurPageID = wpFinished) then
  begin
    Cancel := True;
    Confirm := False;
    Exit;
  end;

  if CurPageID = wpInstalling then
  begin
    Cancel := True;
    Confirm := True;
    Exit;
  end;

  Interrupted := True;
  Cancel := False;
  Confirm := False;

  WizardForm.OuterNotebook.ActivePage := WizardForm.FinishedPage;
  WizardForm.FinishedHeadingLabel.Caption := 'GRAB Setup Wizard was interrupted';
  WizardForm.FinishedLabel.Caption :=
    'GRAB setup was interrupted. Your system has not been modified. No GRAB files or folders were installed.' + #13#10 + #13#10 +
    'Click Finish to exit the Setup Wizard.';
  WizardForm.BackButton.Visible := False;
  WizardForm.CancelButton.Visible := False;
  WizardForm.NextButton.Caption := SetupMessage(msgButtonFinish);
  WizardForm.NextButton.Visible := True;
  WizardForm.NextButton.Enabled := True;
  SendMessageW(WizardForm.NextButton.Handle, BCM_SETSHIELD, 0, 0);
  WizardForm.ActiveControl := WizardForm.NextButton;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    AddDirToSystemPath(ExpandConstant('{app}'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemoveDirFromSystemPath(ExpandConstant('{app}'));
end;
