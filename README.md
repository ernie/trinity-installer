# Trinity Installer

Installs Trinity Engine on this PC or Mac, a Steam Frame, or a Quest or PICO headset: downloads the latest release for the target, copies your own Quake III Arena paks (with the 1.32 patch files if your install lacks them), and where Steam is present adds the library shortcut, Trinity's SteamVR settings and the library artwork.

Requires a Quake III Arena install on this PC, and Developer Mode on a headset target. Builds with Go 1.25 and a C compiler (Fyne).

Windows on ARM64 gets the x64 engine: the installer ships as x64 and runs under emulation there, so it sees an x64 PC.

On Windows, choose "More info > Run anyway" if SmartScreen warns about the unsigned download.

## Targets

The installer installs Trinity on three kinds of target:

- **Steam Frame**: turn on Developer Mode on the headset. When pairing, open Settings > Developer > Pair new host.
- **Quest or PICO**: turn on Developer Mode and USB debugging, connect over USB, and accept the debugging prompt on the headset.
- **This PC or Mac**: nothing beyond the installer.

Every target needs a retail Quake III Arena install with its pak0 files. If the install lacks the 1.32 patch files, the installer downloads them after you accept the id Software license.

## PC install

The PC install goes under a per-user folder: `%LOCALAPPDATA%\Trinity` on Windows, `~/.local/share/trinity` on Linux, and `~/Applications` plus `~/Library/Application Support/Trinity` on macOS.

On Windows and Linux, when Steam (and SteamVR) is installed and the box is checked, the installer also writes a Steam shortcut (and registers Trinity with SteamVR). With several Steam users it adds the shortcut for the one who signed in last; when it cannot tell, the box stays off and says why. Close Steam while the shortcut is written; the entry shows after Steam restarts.

## Third-party software

The installer bundles `adb` from Android platform-tools under the Apache 2.0 license. Its notice is extracted next to adb under the installer's config folder (`TrinityInstaller/adb/<version>/NOTICE.txt`) and is in Google's platform-tools download. Builds embed both after `go run ./tools/fetchadb -out internal/adb/bin`, which CI and release builds run before building.
