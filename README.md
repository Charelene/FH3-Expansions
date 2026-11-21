# FH3 Expansions for Azimuth

Play the **Hot Wheels** and **Blizzard Mountain** expansions of Forza Horizon 3 under
[Azimuth](https://almost-there-games.itch.io/azimuth).

Azimuth runs the Xbox One version of Forza Horizon 3, but it does not accept the game's expansions. This project
converts your own Windows (UWP) copies of the two expansions into the Xbox One format and adds a small mod that lets
Azimuth load them. No game files are included: everything is built on your PC from the copies you own.

This is an unofficial fan project. It is not made by, endorsed by or connected to Microsoft, Xbox Game Studios,
Playground Games, Turn 10 Studios or the makers of Azimuth.

## What you need

- Azimuth **1.0.4** with Forza Horizon 3 added in its launcher. The converter takes the console shaders from that copy
  of the game.
- Your Windows copy of Forza Horizon 3 and of both expansions, dumped from the Microsoft Store version you own, for
  example with [UWPDumper](https://github.com/Wunkolo/UWPDumper).
- About 9 GB of free disk space, about 9 GB of memory, and a few minutes.

## Install

1. Close Azimuth.
2. Download **`FH3-Expansions-Converter-<version>-setup.exe`** from the [latest release](../../releases/latest) and run it.
3. Pick your Azimuth folder (the one with `azimuth.exe`).
4. Pick the folder of your Windows base game, or a folder that holds it. Hot Wheels and Blizzard Mountain are found
   there, or anywhere else on the same drive.
5. Pick where the converted expansions go (next to Azimuth by default) and let it run. A console window shows the
   conversion.
6. Start Azimuth and pick Forza Horizon 3. Both expansions are reached through their travel points on the map.

## What it changes

In your Azimuth folder:

- `dinput8.dll`: the mod. It gives Azimuth the list of accepted Forza Horizon 3 content packages when Azimuth starts,
  and passes DirectInput through to Windows. It does nothing when Forza Horizon 3 is not added in that Azimuth.
- `fh3-expansions.ini`: the two packages the mod lists (their manifest hash, file list digest and version).
- `data\azimuth.ini`: the `content=` line of Forza Horizon 3 now names the converted expansions. The file as it was
  before is kept as `data\azimuth.ini.bak-fh3dlc`.

`azimuth.exe` is not changed.

Your Windows copies are not changed. Files the conversion does not need to touch are hard links to them, not copies.

## Uninstall

Uninstall **FH3 Expansions Converter for Azimuth** from Windows' list of installed apps. It removes the mod, its list
and the converted expansions and puts the `content=` line back.

## Other mods

- **Forza XB1 Extra Options** works alongside it (tested with 0.6.1). Extra Options refuses a modified `azimuth.exe`,
  which is why this project does not modify it.
- If another mod already uses the name `dinput8.dll`, setup keeps it as `dinput8.next.dll` and the expansions mod
  passes DirectInput on to it. Uninstalling puts it back as `dinput8.dll`. Still, try it on a separate Azimuth folder
  or back yours up first.

## Not working?

- `fh3-expansions.log` next to `azimuth.exe` says what the mod did at the last start.
- `data\logs\azimuth.log` should contain a line `content package Forza Horizon 3 ...` for each expansion. A line
  `content folder ... is not given to the game` says why Azimuth refused one.
- If you rebuilt or moved the converted folders, run the setup again.
- Open an [issue](../../issues) and attach both logs.

## How it works

- **Textures.** The Windows packages store every texture in the PC layout. The Xbox One game expects the console
  layout (1D thin tiling), so `converter` re-tiles all zip textures and the track texture streams of the expansions.
- **Shaders.** The Windows track shaders read their material textures and constants through "instanced draw bundles"
  that do not exist on the console. `converter` translates them into the console form; where the Xbox base game has
  the same shader with the same parameters, its console build is used.
- **Hot Wheels island shaders.** On Windows they ship with the base game, so the converter takes them from your base
  game copy.
- **Registration.** Azimuth accepts a content package only when the SHA-256 of its `AppxManifest.xml` is in the
  game's list and the digest of its file list matches; Forza Horizon 3's list is empty. `azimuth-dlc` writes the
  entries for your converted packages into `fh3-expansions.ini`, and the mod (`mod`) puts them into that list in
  memory when Azimuth starts, before it checks its content folders.

## Building

`build.sh` builds everything into `dist\`. It needs [Go](https://go.dev), [mingw-w64](https://www.mingw-w64.org) and
[Inno Setup 6](https://jrsoftware.org/isinfo.php).

| Folder | What |
| --- | --- |
| `converter` | `uwp2xbox`: the converter and `setup`, plus developer commands |
| `azimuth-dlc` | registers the converted packages with an Azimuth folder (`-mod`), and undoes it (`-revert`) |
| `mod` | the `dinput8.dll` mod, in C |
| `installer` | the Inno Setup script |

## Credits

- [Azimuth](https://almost-there-games.itch.io/azimuth) by Almost There Games, which runs the game.
- [UWPDumper](https://github.com/Wunkolo/UWPDumper) by Wunkolo, for dumping your Windows copy.
- [Forza XB1 Extra Options](https://github.com/RokkuDayo) by RokkuDayo, kept working alongside.
- [Inno Setup](https://jrsoftware.org/isinfo.php) by Jordan Russell and Martijn Laan, which builds the installer.
- The BC7 (BPTC) decoder in `converter/bc7.go` follows the layout and tables of the Khronos Data Format
  Specification.
- Built with [Go](https://go.dev) and [mingw-w64](https://www.mingw-w64.org).

## License

The code is GPL-3.0, see [LICENSE](LICENSE). Forza Horizon 3, Hot Wheels and Blizzard Mountain belong to their owners.
