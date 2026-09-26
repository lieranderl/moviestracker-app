; The Windows installer: Moviestracker-Setup-<version>-x64.exe, built with
; Inno Setup 7 by scripts/winapp.sh, which fills the stage folder and passes
;   /DAppVersion=v1.2.3 /DNumericVersion=1.2.3 /DStage=<folder> /DOutputDir=<folder>
;
; It installs for the signed-in user only (no administrator), like dragging
; the Mac app to Applications:
;   %LOCALAPPDATA%\Programs\Moviestracker   the programs
;   %LOCALAPPDATA%\Moviestracker            accounts, settings, TorrServer's data, the log
;
; Silent use (CI, scripted installs):
;   Moviestracker-Setup.exe /VERYSILENT /SUPPRESSMSGBOXES /TASKS=startup
;   unins000.exe /VERYSILENT [/PURGE]      /PURGE also deletes accounts and settings

#ifndef AppVersion
  #error Pass /DAppVersion=v1.2.3 (scripts/winapp.sh does)
#endif

[Setup]
AppId={{6F0C0B7E-8A0E-4E36-9C55-2D8F1C6B7A41}
AppName=Moviestracker
AppVersion={#AppVersion}
AppVerName=Moviestracker {#AppVersion}
AppPublisher=Moviestracker contributors
AppPublisherURL=https://github.com/lieranderl/moviestracker-app
AppSupportURL=https://github.com/lieranderl/moviestracker-app/issues
AppUpdatesURL=https://github.com/lieranderl/moviestracker-app/releases
AppCopyright=AGPL-3.0. Includes TorrServer (GPL-3.0).
VersionInfoVersion={#NumericVersion}
VersionInfoProductVersion={#NumericVersion}
SetupArchitecture=x64
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
PrivilegesRequired=lowest
DefaultDirName={autopf}\Moviestracker
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableReadyPage=yes
; Moviestracker.exe --quit stops the running app before files are replaced.
CloseApplications=no
RestartApplications=no
SetupIconFile=..\..\cmd\tray\moviestracker.ico
UninstallDisplayIcon={app}\Moviestracker.exe
UninstallDisplayName=Moviestracker
WizardStyle=modern
Compression=lzma2/max
SolidCompression=yes
OutputDir={#OutputDir}
OutputBaseFilename=Moviestracker-Setup-{#AppVersion}-x64

[Tasks]
Name: startup; Description: "Start Moviestracker when I sign in"
Name: desktopicon; Description: "Add a shortcut to the desktop"; Flags: unchecked

[Files]
Source: "{#Stage}\Moviestracker.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\moviestracker-server.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\torrserver.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\LICENSE.txt"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\NOTICE.txt"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\licenses\*"; DestDir: "{app}\licenses"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Moviestracker"; Filename: "{app}\Moviestracker.exe"
Name: "{autodesktop}\Moviestracker"; Filename: "{app}\Moviestracker.exe"; Tasks: desktopicon

[Registry]
; The tray app's "Start when I sign in" turns this value on and off.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "Moviestracker"; ValueData: """{app}\Moviestracker.exe"""; Tasks: startup; Flags: uninsdeletevalue

[Run]
Filename: "{app}\Moviestracker.exe"; Description: "Open Moviestracker"; Flags: nowait postinstall

[UninstallDelete]
; The ones the programs leave next to themselves.
Type: filesandordirs; Name: "{app}"

[Code]
// StopMoviestracker asks a running Moviestracker to quit and waits until it,
// its server and TorrServer have stopped, so their files can be replaced or
// removed.
procedure StopMoviestracker(Tray: String);
var
  Code: Integer;
begin
  if not FileExists(Tray) then
    exit;
  if Exec(Tray, '--quit', '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0) then
    exit;
  // It did not stop in time: end what this install runs.
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /T /IM Moviestracker.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /T /IM moviestracker-server.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  StopMoviestracker(ExpandConstant('{app}\Moviestracker.exe'));
  Result := '';
end;

function PurgeRequested(): Boolean;
var
  I: Integer;
begin
  Result := False;
  for I := 1 to ParamCount do
    if CompareText(ParamStr(I), '/PURGE') = 0 then
      Result := True;
end;

// RemoveGStreamerCache deletes the GStreamer TorrServer unpacks from itself
// into %LOCALAPPDATA%\TorrServer\gst-lib-<hash>, and the folder when empty.
procedure RemoveGStreamerCache();
var
  Root: String;
  Found: TFindRec;
begin
  Root := ExpandConstant('{localappdata}\TorrServer');
  if FindFirst(Root + '\gst-lib-*', Found) then
  begin
    try
      repeat
        DelTree(Root + '\' + Found.Name, True, True, True);
      until not FindNext(Found);
    finally
      FindClose(Found);
    end;
  end;
  RemoveDir(Root);
end;

// Uninstalling keeps accounts and settings (like the Mac app and Linux),
// unless the person asks, or /PURGE is given; TorrServer's torrent list and
// the GStreamer it unpacked always go.
procedure CurUninstallStepChanged(Step: TUninstallStep);
var
  Data: String;
  Purge: Boolean;
begin
  if Step <> usUninstall then
    exit;
  StopMoviestracker(ExpandConstant('{app}\Moviestracker.exe'));
  Data := ExpandConstant('{localappdata}\Moviestracker');
  Purge := PurgeRequested();
  if not Purge and not UninstallSilent() and DirExists(Data) then
    Purge := MsgBox('Also delete your Moviestracker accounts and settings?' + #13#10#13#10 +
      'Keep them to find everything as it was if you install Moviestracker again.',
      mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
  DelTree(Data + '\engine', True, True, True);
  RemoveGStreamerCache();
  if Purge then
    DelTree(Data, True, True, True);
end;
