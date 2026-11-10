; SPDX-License-Identifier: AGPL-3.0-or-later
; Installer Windows di VeronaTAO (Inno Setup 6).
; Compilare prima l'eseguibile con packaging/build.sh, poi:
;   ISCC /DAppVersion=0.1.0 packaging\veronatao.iss

#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif

[Setup]
AppId={{6E0B4C0B-6D0C-4C5C-9A35-7E1E7C0B7A10}
AppName=VeronaTAO
AppVersion={#AppVersion}
AppPublisher=Autori di VeronaTAO
DefaultDirName={autopf}\VeronaTAO
DefaultGroupName=VeronaTAO
DisableProgramGroupPage=yes
LicenseFile=..\LICENSE
OutputDir=..\dist
OutputBaseFilename=VeronaTAO-Setup
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
CloseApplications=yes

[Languages]
Name: "italian"; MessagesFile: "compiler:Languages\Italian.isl"

[Tasks]
Name: "desktopicon"; Description: "Crea un collegamento sul desktop"
Name: "autostart"; Description: "Avvia VeronaTAO all'accensione del computer (consigliato se altre postazioni si collegano a questo computer)"; Flags: unchecked
Name: "firewall"; Description: "Consenti l'accesso dalle altre postazioni della rete locale (regola del firewall per le sole reti private)"; Flags: unchecked

[Dirs]
; L'archivio sta fuori dalla cartella del programma e non viene toccato da aggiornamenti o disinstallazione.
Name: "{commonappdata}\VeronaTAO"; Permissions: users-modify; Flags: uninsneveruninstall

[Files]
Source: "..\dist\VeronaTAO.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; DestName: "LICENZA.txt"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; DestName: "LEGGIMI.md"; Flags: ignoreversion
Source: "..\REGULATORY.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\COMPLIANCE.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\VeronaTAO"; Filename: "{app}\VeronaTAO.exe"
Name: "{commondesktop}\VeronaTAO"; Filename: "{app}\VeronaTAO.exe"; Tasks: desktopicon
Name: "{commonstartup}\VeronaTAO"; Filename: "{app}\VeronaTAO.exe"; Parameters: "-senza-browser"; Tasks: autostart

[Run]
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall add rule name=""VeronaTAO"" dir=in action=allow program=""{app}\VeronaTAO.exe"" profile=private"; Flags: runhidden; Tasks: firewall
Filename: "{app}\VeronaTAO.exe"; Description: "Avvia VeronaTAO"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/IM VeronaTAO.exe /F"; Flags: runhidden; RunOnceId: "StopVeronaTAO"
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall delete rule name=""VeronaTAO"""; Flags: runhidden; RunOnceId: "FirewallVeronaTAO"
