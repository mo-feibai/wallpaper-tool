; 壁纸工具安装包脚本 (Inno Setup 7)
; 构建: build-installer.ps1 (iscc /DMyAppVersion=x.y.z installer\wallpaper-tool.iss)

#define MyAppName "壁纸工具"
#define MyAppExeName "wallpaper-tool.exe"
#ifndef MyAppVersion
#define MyAppVersion "0.2.0"
#endif

[Setup]
AppId={{8E5A2C31-6B7D-4F0E-9A44-2C1D53F0B7A1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
DefaultDirName={autopf}\WallpaperTool
; PrivilegesRequired=lowest + {autopf} => 按用户安装到 %LOCALAPPDATA%\Programs, 全程无 UAC
PrivilegesRequired=lowest
UninstallDisplayName={#MyAppName}
UninstallDisplayIcon={app}\{#MyAppExeName}
SetupIconFile=..\internal\assets\icon.ico
OutputDir=..\dist
OutputBaseFilename=WallpaperTool-setup-{#MyAppVersion}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ShowLanguageDialog=no
CloseApplications=no
DisableProgramGroupPage=yes
DefaultGroupName={#MyAppName}

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce

[Files]
Source: "..\wallpaper-tool.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\卸载 {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "立即运行 {#MyAppName}"; Flags: nowait postinstall skipifsilent

[Code]
const
  AppMutexName = 'Local\WallpaperToolSingleInstance';
  RunValueName = 'WallpaperTool';
  AumidKeyPath = 'Software\Classes\AppUserModelId\WallpaperTool.App';
  TaskName = 'WallpaperToolLockScreen';

var
  RemoveDataChk: TNewCheckBox;

// 静默结束运行中的实例(升级/卸载前调用), 忽略一切错误
procedure KillRunningInstance;
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'),
    '/IM {#MyAppExeName} /F', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  KillRunningInstance;
end;

function InitializeUninstall: Boolean;
begin
  Result := True;
  KillRunningInstance;
end;

// 卸载开始时(窗体已可见)创建"删除数据"复选框
procedure CreateRemoveDataCheckbox;
var
  Chk: TNewCheckBox;
begin
  Chk := TNewCheckBox.Create(UninstallProgressForm);
  Chk.Parent := UninstallProgressForm;
  Chk.Left := ScaleX(10);
  Chk.Top := UninstallProgressForm.StatusLabel.Top +
    UninstallProgressForm.StatusLabel.Height + ScaleY(12);
  Chk.Width := ScaleX(400);
  Chk.Caption := '同时删除配置与缓存(设置、历史、壁纸文件)';
  Chk.Checked := False;
  RemoveDataChk := Chk;
end;

// 卸载收尾: 清理计划任务 / 自启 Run 值 / 通知身份键 / 可选删除数据
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
    CreateRemoveDataCheckbox;

  if CurUninstallStep <> usPostUninstall then
    Exit;

  // 计划任务(可能不存在, 忽略结果)
  Exec(ExpandConstant('{sys}\schtasks.exe'),
    '/Delete /TN ' + TaskName + ' /F', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  // 自启 Run 值
  RegDeleteValue(HKEY_CURRENT_USER,
    'Software\Microsoft\Windows\CurrentVersion\Run', RunValueName);
  // 通知身份注册表键
  RegDeleteKeyIncludingSubkeys(HKEY_CURRENT_USER, AumidKeyPath);

  // 可选: 删除全部用户数据
  if (RemoveDataChk <> nil) and RemoveDataChk.Checked then
    DelTree(ExpandConstant('{userappdata}\wallpaper-tool'), True, True, True);
end;
