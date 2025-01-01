# eve-chainkills  
Listener to zkill websocket for kills of interest in wormhole chain

## Overview  
The **eve-chainkills** application uses a real-time WebSocket feed from [zKillboard](https://zkillboard.com/) to detect kill events of interest (for example, wormhole chain kills or specific corporate/alliance kills) and sends notifications to Discord via webhooks. It also queries an internal API (or ESI) to gather system and character details, removing the need for direct database usage.

## Key Components
- **main.go**  
  - The entry point of the application. It loads configuration from `config.json`, sets up a logger, starts the `ChainKillChecker`, and handles graceful shutdown (listening for OS signals).
- **chainkillchecker.go**  
  - Connects to the zKillboard WebSocket feed. Receives kill data, determines whether it concerns monitored corporations, alliances, or wormhole systems, and triggers relevant Discord notifications.  
  - Maintains system and character info via your custom API endpoints.
- **killdetails.go**  
  - Fetches additional kill info (e.g., victim character details) from ESI, using `killmail_id` and `hash`.
- **killembed.go**  
  - Creates Discord "embed" objects to format kill or loss notifications with consistent colors and structure.
- **discord.go**  
  - Contains helper logic to send text or embed messages to a Discord webhook.
- **config.go**  
  - Parses and validates `config.json` into a strongly typed `AppConfig`.

## Architecture Overview
1. **Configuration** (`config.json`):  
   - Stores details such as the slug for your internal API, authentication tokens, Discord webhook credentials, and tracked corp/alliance IDs.

2. **zKillboard Connection**:  
   - The application opens a persistent WebSocket to `wss://zkillboard.com/websocket/`. It subscribes to `killstream` events so it receives kill data in real-time.  
   - Automatically attempts reconnection if the socket is lost.

3. **Kill Event Handling** (`handleZKillMessage` in `chainkillchecker.go`):  
   - Checks if the kill is relevant to your tracked alliances/corps or wormhole systems.  
   - Optionally fetches extended kill info from ESI in `killdetails.go`.  
   - Posts notifications to Discord (chain kills vs. corp kills) via webhooks.

4. **Discord Integration**:  
   - Uses minimal JSON payloads to send either a plain text message or a richer embed with color-coded highlights.

## Installation
1. [Installation](#installation)

### Prerequisites
- [docker](https://docs.docker.com/)
- [docker-compose](https://docs.docker.com/)

> **Note**: The Docker-compose file uses Compose v3.8, so requires Docker Engine 19.03.0+

### Steps

1. **Clone the repo**  
   ```shell
   git clone https://github.com/commando29/eve-chainkills.git/
   ```

1. **Edit the config.json**  
   - Use the `config.example.json` as a template
   - Set your Discord webhook IDs/tokens, your API base URL/token, and any tracked corp/alliance IDs.

1. **Build & Run**  
   ```shell
   docker-compose up -d --build
   ```

Once the containers start, the application will connect to zKillboard and begin listening for kills. You should see logs in the Docker container output, and relevant notifications appearing in Discord.

## License  
This project is licensed under the [MIT License](LICENSE) - see the LICENSE file for details.

## Contributing  
Pull requests and issues are welcome! For major changes, please open an issue first to discuss what you would like to change.

## Contact  
For additional help or questions, feel free to reach out via GitHub Issues.  
