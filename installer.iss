[Setup]
AppName=VRShare
AppVersion=1.0.0
AppPublisher=Vexedaa
AppPublisherURL=https://github.com/vexedaa/vrshare
DefaultDirName={autopf}\VRShare
DefaultGroupName=VRShare
UninstallDisplayIcon={app}\vrshare.exe
OutputBaseFilename=VRShare-Setup
OutputDir=.
Compression=lzma2/ultra64
SolidCompression=yes
SetupIconFile=assets\icon.ico
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
WizardStyle=modern

[Files]
Source: "vrshare.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "bundle\ffmpeg\*"; DestDir: "{app}\ffmpeg"; Flags: ignoreversion recursesubdirs
Source: "LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\VRShare"; Filename: "{app}\vrshare.exe"
Name: "{group}\Uninstall VRShare"; Filename: "{uninstallexe}"
Name: "{autodesktop}\VRShare"; Filename: "{app}\vrshare.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Run]
Filename: "{app}\vrshare.exe"; Description: "Launch VRShare"; Flags: nowait postinstall skipifsilent
