# Trinity Installer

Installs Trinity Engine on this PC or Mac, a Steam Frame, or a Quest or PICO headset: downloads the latest release for the target, copies your own Quake III Arena paks (with the 1.32 patch files if your install lacks them), and where Steam is present adds the library shortcut, Trinity's SteamVR settings and the library artwork.

Requires a Quake III Arena install on this PC, and Developer Mode on a headset target. Builds with Go 1.27 and a C compiler (Fyne).

Windows on ARM64 gets the x64 engine: the installer ships as x64 and runs under emulation there, so it sees an x64 PC.

On Windows, choose "More info > Run anyway" if SmartScreen warns about the unsigned download.

## Targets

The installer installs Trinity on three kinds of target:

- **Steam Frame**: turn on Developer Mode on the headset. When pairing, open Settings > Developer > Pair new host. Trinity always starts in VR there, without a mirror window.
- **Quest or PICO**: turn on Developer Mode and USB debugging, connect over USB, and accept the debugging prompt on the headset.
- **This PC or Mac**: nothing beyond the installer.

Every target needs a retail Quake III Arena install with its pak0 files. If the install lacks the 1.32 patch files, the installer downloads them after you accept the id Software license.

## PC install

The PC install goes to `C:\Games\Trinity` on Windows (no administrator rights needed; pick another folder on the Destination screen if you prefer), and under a per-user folder elsewhere: `~/.local/share/trinity` on Linux, and `~/Applications` plus `~/Library/Application Support/Trinity` on macOS.

On Windows and Linux, the Destination screen asks how you want to play, VR or Flatscreen (VR is preselected when SteamVR is installed). Every place you tick, "Add to Start Menu" ("Add to applications menu" on Linux), "Add to Desktop" and "Add to Steam", gets a "Trinity" shortcut that starts in that mode, plus "Trinity (Flat)" or "Trinity (VR)" for the other mode unless you untick "Also create ... shortcuts". A shortcut sets the mode for that launch only; starting the game without one uses the mode last chosen in Trinity's menu.

"Add to Steam" is offered when Steam is installed. The Steam shortcuts get library artwork, and the VR one is flagged "Include in VR Library", so it shows in SteamVR and starts in the headset; with Flatscreen chosen and no VR shortcuts, Trinity does not appear in SteamVR. With several Steam users the installer adds the shortcuts for the one who signed in last; when it cannot tell, the box stays off and says why. If Steam is running, the installer closes it to write the shortcuts and starts it again when the install finishes.

## Uninstalling

On Windows, remove Trinity from Settings > Apps > Trinity. Close Trinity first; if Steam is running, the uninstaller closes it and starts it again afterwards (the silent `--quiet` uninstall refuses instead). The uninstaller removes the files it installed and the folders it created once they are empty (anything you added stays, along with its folder), and the Start Menu, Desktop and Steam entries. Trinity keeps its settings, downloads, screenshots and demos in the install folder, and ticking "Also delete my settings, downloads and everything else in the Trinity folder" removes everything there. If you installed into a folder that already existed, the box removes only Trinity's own settings files and its screenshot, demo, video and TV folders, and leaves the paks, since the uninstaller cannot tell downloaded paks from yours. On Linux, delete the install folder, the `trinity.desktop`, `trinity-vr.desktop` and `trinity-flat.desktop` files in `~/.local/share/applications` and on your Desktop, and the Trinity shortcuts in Steam. On macOS, drag Trinity.app to the Trash; its paks and settings are in `~/Library/Application Support/Trinity`.

## Third-party software

The installer bundles `adb` from Android platform-tools under the Apache 2.0 license. Its notice is extracted next to adb under the installer's config folder (`TrinityInstaller/adb/<version>/NOTICE.txt`) and is in Google's platform-tools download. Builds embed both after `go run ./tools/fetchadb -out internal/adb/bin`, which CI and release builds run before building.

## Building locally

The toolchain is Go 1.27, pinned in `mise.toml`; `mise install` fetches it. go.mod also names the toolchain, so an older `go` on the path downloads it on first use.

The release workflow packages the Windows build with `fyne package`, which hides the console. A plain `go build` shows one; build the Windows exe with:

```
go run ./tools/fetchadb -out internal/adb/bin
go build -ldflags "-H windowsgui -X main.version=dev" -o build/trinity-installer.exe ./cmd/trinity-installer
```
