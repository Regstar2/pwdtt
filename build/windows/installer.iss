#ifndef AppVersion
  #define AppVersion "dev"
#endif

#ifndef VersionInfoVersion
  #define VersionInfoVersion "0.0.0.0"
#endif

#ifndef SourceExe
  #define SourceExe "..\..\build\bin\pwdtt-windows-amd64.exe"
#endif

#ifndef OutputDir
  #define OutputDir "..\..\build\bin"
#endif

#ifndef OutputBaseFilename
  #define OutputBaseFilename "pwdtt-windows-amd64-setup"
#endif

[Setup]
AppId={{D8B32718-4A7F-4D58-95D8-531F9AF62210}
AppName=PWDTT
AppVersion={#AppVersion}
AppVerName=PWDTT {#AppVersion}
AppPublisher=Regstar2
AppPublisherURL=https://github.com/Regstar2/pwdtt
AppSupportURL=https://github.com/Regstar2/pwdtt/issues
AppUpdatesURL=https://github.com/Regstar2/pwdtt/releases
DefaultDirName={localappdata}\Programs\PWDTT
DefaultGroupName=PWDTT
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename={#OutputBaseFilename}
SetupIconFile=icon.ico
UninstallDisplayIcon={app}\PWDTT.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
UsePreviousAppDir=yes
VersionInfoVersion={#VersionInfoVersion}
VersionInfoCompany=Regstar2
VersionInfoDescription=PWDTT installer
VersionInfoProductName=PWDTT
VersionInfoProductVersion={#VersionInfoVersion}

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "PWDTT.exe"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\PWDTT"; Filename: "{app}\PWDTT.exe"; WorkingDir: "{app}"

[UninstallDelete]
Type: filesandordirs; Name: "{app}\logs"
