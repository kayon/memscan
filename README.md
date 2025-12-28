# memscan
A high-performance memory scanning engine specifically designed for the Steam Deck.

## About
**memscan** serves as the core backend implementation for the [Reroll](https://github.com/kayon/decky-reroll) plugin, part of the [Decky Loader](https://github.com/SteamDeckHomebrew/decky-loader) ecosystem.

## Project Structure

- **/cmd/backend**: The primary backend service utilized by the [Reroll](https://github.com/kayon/decky-reroll) plugin.
- **/cmd/memscan-cli**: A standalone command-line interface (CLI) version for rapid functional testing and development debugging.

## License
This project is licensed under the **GNU General Public License v3.0**. For more details, please refer to the [LICENSE](LICENSE) file.

This project also utilizes several third-party open-source components (e.g., color, pflag, promptui) under their respective MIT and BSD licenses. Please refer to `go.mod` for a complete list of dependencies.
