// How MyGo's CLI packages the desktop app (`make desktop`, `make desktop-web`).
// The fields are MyGo's: https://github.com/egoist/mygo/blob/main/docs/configuration.md
export default {
  name: "AMDL",
  // The packages' file names: apart from the amdl command line's release
  // archives, and the Linux executable never shadows the amdl command.
  slug: "amdl-desktop",
  // macOS keys the app's permissions and window state on it: keep it.
  identifier: "me.lyjw.amdl",
  // The Makefile sets it from VERSION, without the leading v.
  version: process.env.DESKTOP_VERSION || "0.0.0",
  icon: "icon.png",
  out: "build",
  linux: {
    comment: "Download music from Apple Music",
    categories: ["AudioVideo", "Audio"],
    // Native UI only: the package needs GTK, not WebKitGTK.
    webview: false,
  },
  windows: {
    // %LOCALAPPDATA%\Programs\AMDL would be the command line's own
    // Programs\amdl, as Windows ignores case: its installer would replace
    // amdl.exe and its uninstaller remove the folder.
    installDirName: "AMDL Desktop",
  },
};
