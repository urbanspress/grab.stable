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
WelcomeLabel2=The Setup Wizard will install GRAB v1.1 on your computer. Click Next to continue or Cancel to exit the Setup Wizard.
WizardLicense=End-User License Agreement
LicenseLabel=Please read the following License Agreement carefully.
LicenseLabel3=Please read the following License Agreement. You must accept the terms of this agreement before continuing with the installation.
LicenseAccepted=I &accept the terms in the License Agreement
LicenseNotAccepted=I &do not accept the terms in the License Agreement
WizardSelectDir=Destination Folder
SelectDirDesc=Where should GRAB be installed?
SelectDirLabel3=Install GRAB to:
SelectDirBrowseLabel=
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
  LicenseAcceptCheck: TNewCheckBox;
  ToolsPage: TWizardPage;
  DockerCheck: TNewCheckBox;
  GitCheck: TNewCheckBox;
  GoCheck: TNewCheckBox;

function SendMessageW(hWnd: HWND; Msg: UINT; wParam: LongInt; lParam: LongInt): LongInt;
  external 'SendMessageW@user32.dll stdcall';

function NormalizeDir(const S: string): string;
begin
  Result := RemoveBackslashUnlessRoot(Trim(S));
end;

function IsDockerInstalled(): Boolean;
begin
  Result :=
    FileExists(ExpandConstant('{commonpf}\Docker\Docker\Docker Desktop.exe')) or
    FileExists(ExpandConstant('{commonpf32}\Docker\Docker\Docker Desktop.exe')) or
    (FileSearch('docker.exe', GetEnv('PATH')) <> '') or
    RegKeyExists(HKEY_LOCAL_MACHINE, 'SOFTWARE\Docker Inc.\Docker\1.0') or
    RegKeyExists(HKEY_LOCAL_MACHINE, 'SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Docker Desktop');
end;

function IsGitInstalled(): Boolean;
begin
  Result :=
    FileExists(ExpandConstant('{commonpf}\Git\cmd\git.exe')) or
    FileExists(ExpandConstant('{commonpf32}\Git\cmd\git.exe')) or
    (FileSearch('git.exe', GetEnv('PATH')) <> '') or
    RegKeyExists(HKEY_LOCAL_MACHINE, 'SOFTWARE\GitForWindows') or
    RegKeyExists(HKEY_CURRENT_USER, 'SOFTWARE\GitForWindows');
end;

function IsGoInstalled(): Boolean;
begin
  Result :=
    FileExists(ExpandConstant('{commonpf}\Go\bin\go.exe')) or
    FileExists('C:\Go\bin\go.exe') or
    (FileSearch('go.exe', GetEnv('PATH')) <> '') or
    RegKeyExists(HKEY_LOCAL_MACHINE, 'SOFTWARE\GoProgrammingLanguage');
end;

procedure LicenseAcceptCheckClick(Sender: TObject);
begin
  if LicenseAcceptCheck.Checked then
  begin
    WizardForm.LicenseAcceptedRadio.Checked := True;
    WizardForm.NextButton.Enabled := True;
  end
  else
  begin
    WizardForm.LicenseNotAcceptedRadio.Checked := True;
    WizardForm.NextButton.Enabled := False;
  end;
end;

function CreateToolRow(const ParentPage: TWizardPage; const TopPos: Integer;
  const BaseLabel, TooltipText: string; const AlreadyInstalled: Boolean): TNewCheckBox;
var
  CheckBox: TNewCheckBox;
  InfoBadge: TLabel;
begin
  CheckBox := TNewCheckBox.Create(ParentPage);
  CheckBox.Parent := ParentPage.Surface;
  CheckBox.Left := 0;
  CheckBox.Top := TopPos;
  CheckBox.Width := ParentPage.SurfaceWidth - ScaleX(38);
  CheckBox.Height := ScaleY(22);
  CheckBox.Checked := False;
  if AlreadyInstalled then
  begin
    CheckBox.Caption := BaseLabel + ' (Already installed)';
    CheckBox.Enabled := False;
  end
  else
  begin
    CheckBox.Caption := BaseLabel;
    CheckBox.Enabled := True;
  end;

  InfoBadge := TLabel.Create(ParentPage);
  InfoBadge.Parent := ParentPage.Surface;
  InfoBadge.AutoSize := False;
  InfoBadge.Left := ParentPage.SurfaceWidth - ScaleX(28);
  InfoBadge.Top := TopPos + ScaleY(2);
  InfoBadge.Width := ScaleX(24);
  InfoBadge.Height := ScaleY(18);
  InfoBadge.Caption := '(i)';
  InfoBadge.Alignment := taCenter;
  InfoBadge.Transparent := False;
  InfoBadge.Color := $00D47800;
  InfoBadge.Font.Color := clWhite;
  InfoBadge.Font.Style := [fsBold];
  InfoBadge.Font.Size := 8;
  InfoBadge.ShowHint := True;
  InfoBadge.Hint := TooltipText;
  InfoBadge.Cursor := crHand;

  Result := CheckBox;
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
var
  HintLabel: TNewStaticText;
begin
  Interrupted := False;
  WizardForm.ShowHint := True;

  // Screen 2: Replace two radio buttons with a single acceptance checkbox
  WizardForm.LicenseAcceptedRadio.Visible := False;
  WizardForm.LicenseNotAcceptedRadio.Visible := False;
  WizardForm.LicenseMemo.Height :=
    WizardForm.LicenseNotAcceptedRadio.Top - WizardForm.LicenseMemo.Top - ScaleY(8);

  LicenseAcceptCheck := TNewCheckBox.Create(WizardForm.LicensePage);
  LicenseAcceptCheck.Parent := WizardForm.LicensePage;
  LicenseAcceptCheck.Left := WizardForm.LicenseMemo.Left;
  LicenseAcceptCheck.Top := WizardForm.LicenseMemo.Top + WizardForm.LicenseMemo.Height + ScaleY(12);
  LicenseAcceptCheck.Width := WizardForm.LicenseMemo.Width;
  LicenseAcceptCheck.Height := ScaleY(22);
  LicenseAcceptCheck.Caption := 'I accept the terms in the License Agreement';
  LicenseAcceptCheck.Checked := False;
  LicenseAcceptCheck.OnClick := @LicenseAcceptCheckClick;

  // Screen 3: Hide yellow folder icon and place clean "Install GRAB to:" label above path box
  WizardForm.SelectDirBitmapImage.Visible := False;
  WizardForm.SelectDirBrowseLabel.Visible := False;
  WizardForm.SelectDirLabel.Left := WizardForm.DirEdit.Left;
  WizardForm.SelectDirLabel.Width :=
    WizardForm.DirEdit.Width + WizardForm.DirBrowseButton.Width + ScaleX(8);
  WizardForm.SelectDirLabel.Top := WizardForm.DirEdit.Top - ScaleY(22);
  WizardForm.SelectDirLabel.Caption := 'Install GRAB to:';

  // Screen 4: Tools for GRAB Ecosystem
  ToolsPage := CreateCustomPage(
    wpSelectDir,
    'Tools for GRAB Ecosystem',
    'Select any optional companion tools you would like to install. Already-installed tools are detected automatically.'
  );

  DockerCheck := CreateToolRow(
    ToolsPage,
    ScaleY(12),
    'Docker Desktop',
    'Runs hardened, read-only Grisp production containers and local Redis, PostgreSQL, or MariaDB databases for GRAB apps.',
    IsDockerInstalled()
  );

  GitCheck := CreateToolRow(
    ToolsPage,
    ScaleY(50),
    'Git for Windows',
    'Clones GRAB starter templates, tracks app version history, and powers grabman ecosystem updates.',
    IsGitInstalled()
  );

  GoCheck := CreateToolRow(
    ToolsPage,
    ScaleY(88),
    'Go Toolchain (Optional — For Source Contributors)',
    'Only needed if you plan to modify or compile GRAB from source; running pre-built GRAB apps does not require Go.',
    IsGoInstalled()
  );

  HintLabel := TNewStaticText.Create(ToolsPage);
  HintLabel.Parent := ToolsPage.Surface;
  HintLabel.Left := 0;
  HintLabel.Top := ScaleY(134);
  HintLabel.Width := ToolsPage.SurfaceWidth;
  HintLabel.WordWrap := True;
  HintLabel.Caption := 'Hover over any (i) badge for details. Checked tools will be installed via winget after GRAB setup finishes.';
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if CurPageID = wpLicense then
  begin
    WizardForm.NextButton.Enabled := LicenseAcceptCheck.Checked;
  end;

  if CurPageID = wpReady then
    SendMessageW(WizardForm.NextButton.Handle, BCM_SETSHIELD, 0, 1)
  else
    SendMessageW(WizardForm.NextButton.Handle, BCM_SETSHIELD, 0, 0);
end;

function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo, MemoTypeInfo,
  MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: string): string;
var
  ToolsSummary: string;
begin
  Result := '';
  if MemoDirInfo <> '' then
    Result := Result + MemoDirInfo + NewLine + NewLine;

  Result := Result + 'System integration:' + NewLine +
    Space + 'Add GRAB to the system PATH' + NewLine +
    Space + 'Register GRAB {#AppVersion} in Windows Installed Apps';

  ToolsSummary := '';
  if DockerCheck.Enabled and DockerCheck.Checked then
    ToolsSummary := ToolsSummary + NewLine + Space + 'Install Docker Desktop (via winget)';
  if GitCheck.Enabled and GitCheck.Checked then
    ToolsSummary := ToolsSummary + NewLine + Space + 'Install Git for Windows (via winget)';
  if GoCheck.Enabled and GoCheck.Checked then
    ToolsSummary := ToolsSummary + NewLine + Space + 'Install Go Toolchain (via winget)';

  if ToolsSummary <> '' then
    Result := Result + NewLine + NewLine + 'Optional GRAB Ecosystem tools:' + ToolsSummary;
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

procedure RunSelectedToolInstalls();
var
  Cmd: string;
  ResultCode: Integer;
begin
  Cmd := '';
  if DockerCheck.Enabled and DockerCheck.Checked then
    Cmd := Cmd + 'Write-Host ''Installing Docker Desktop...''; winget install -e --id Docker.DockerDesktop --accept-source-agreements --accept-package-agreements; ';
  if GitCheck.Enabled and GitCheck.Checked then
    Cmd := Cmd + 'Write-Host ''Installing Git for Windows...''; winget install -e --id Git.Git --accept-source-agreements --accept-package-agreements; ';
  if GoCheck.Enabled and GoCheck.Checked then
    Cmd := Cmd + 'Write-Host ''Installing Go Toolchain...''; winget install -e --id GoLang.Go --accept-source-agreements --accept-package-agreements; ';

  if Cmd <> '' then
  begin
    Exec(
      'powershell.exe',
      '-NoProfile -ExecutionPolicy Bypass -Command "' + Cmd + 'Write-Host ''''; Read-Host ''Optional tool setup finished. Press Enter to close''"',
      '',
      SW_SHOWNORMAL,
      ewWaitUntilTerminated,
      ResultCode
    );
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    AddDirToSystemPath(ExpandConstant('{app}'));
    RunSelectedToolInstalls();
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemoveDirFromSystemPath(ExpandConstant('{app}'));
end;
