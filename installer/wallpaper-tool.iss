; 壁纸工具安装包脚本 (Inno Setup 7)
; 构建: build-installer.ps1 (iscc /DMyAppVersion=x.y.z installer\wallpaper-tool.iss)

#define MyAppName "壁纸工具"
#define MyAppExeName "wallpaper-tool.exe"
#ifndef MyAppVersion
#define MyAppVersion "0.4.0"
#endif

[Setup]
AppId={{8E5A2C31-6B7D-4F0E-9A44-2C1D53F0B7A1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
; 显式指定用户级目录: {autopf} 会随系统盘/管理员身份解析到意外位置(实测落到 D:\)
DefaultDirName={localappdata}\Programs\WallpaperTool
; 按用户安装, 安装/升级/卸载全程无 UAC
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
; checkedonce: 升级时不重建桌面快捷方式, 保留用户可能做过的自定义。
; 图标显示不依赖重建: IconLocation=",0" 即"使用目标 exe 的第 0 个图标",
; exe 已内嵌图标资源(.syso), 故快捷方式图标始终正确。
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce

[Files]
Source: "..\wallpaper-tool.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
; IconFilename 必须显式指定: SetupIconFile 只作用于 setup.exe 自身, 不会传给快捷方式。
; 指向已安装的 exe(自带图标资源), 不依赖构建期相对路径, 每次安装/升级都稳定生效。
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\卸载 {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

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
