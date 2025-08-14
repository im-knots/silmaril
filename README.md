# Silmaril - P2P AI Model Distribution

Silmaril is a fully decentralized peer-to-peer system for sharing and discovering AI models using BitTorrent. Unlike traditional model hubs that rely on centralized servers, Silmaril distributes both the models AND the discovery catalog through the BitTorrent network itself. This means no central point of failure, no single organization controlling access, and true peer-to-peer model sharing. 

The system uses a "catalog-as-torrent" approach where the model catalog is itself a torrent, with only a tiny reference stored in the DHT. This allows unlimited scaling while maintaining complete decentralization anyone can share models, anyone can discover them, and the network keeps running as long as peers are online.

Note: This is a work in progress. Silmaril network stability and model catalog persistence will be addressed with bootstrap nodes in the future

## Features

- **P2P Distribution**: Share models using BitTorrent protocol with DHT discovery
- **Catalog-as-Torrent**: Scalable discovery system using BitTorrent to distribute the model catalog itself
- **BEP 44 Discovery**: Decentralized model discovery using DHT mutable items (no central tracker needed)
- **Tag-based Search**: Find models by tags like "llama", "mistral", "7b", etc.
- **Smart Caching**: Efficient catalog caching to minimize network requests


## Installation

### From Source

```bash
git clone https://github.com/yourusername/silmaril.git
cd silmaril
make build
```

The binary will be available at `build/silmaril`.

## Using Silmaril

### Command Reference

| Command | Description |
|---------|-------------|
| **Initialize** | |
| `silmaril init` | Set up directories and configuration |
| `silmaril init --path /custom/location` | Initialize in custom location |
| `silmaril init --cleanup` | Remove Silmaril and all models |
| **Daemon Management** | |
| `silmaril daemon start` | Start the P2P daemon |
| `silmaril daemon status` | Check daemon status |
| `silmaril daemon stop` | Stop the daemon |
| **Discovery & Download** | |
| `silmaril discover` | Search all available models |
| `silmaril discover [pattern]` | Search for specific models |
| `silmaril get [model]` | Download a model |
| `silmaril list` | List local models |
| **Sharing Models** | |
| `silmaril share --all` | Share all downloaded models |
| `silmaril share [model]` | Share specific model from registry |
| `silmaril share [url]` | Clone and share from repository |
| `silmaril share [path] --name [org/model] --license [license]` | Publish from local directory |
| **Help** | |
| `silmaril help` | Show help information |

### Share Command Options

| Option | Description | Default |
|--------|-------------|---------|
| `--all` | Share all downloaded models | false |
| `--name` | Model name when publishing from directory | required for local dirs |
| `--license` | License for new models | required for local dirs |
| `--version` | Model version | main |
| `--branch` | Git branch to clone | main |
| `--depth` | Git clone depth (0 for full) | 1 |
| `--skip-lfs` | Skip Git LFS files | false |
| `--skip-dht` | Skip DHT announcement | false |
| `--piece-length` | Torrent piece size | 4MB |
| `--sign` | Sign the manifest | true |
| `--no-monitor` | Don't monitor after sharing | true |

### Important Notes

- **Daemon Required**: Start the daemon before running other commands (`silmaril daemon start`)
- **Repository URLs**: Use full URLs for git repositories to trigger cloning
- **Storage Location**: Models are stored in `~/.silmaril/models/` by default
- **Configuration**: Settings are in `~/.config/silmaril/config.yaml`

## API Reference

The Silmaril daemon exposes a REST API on port 8737 (configurable). The CLI is a thin client that uses these endpoints:

| Method | Endpoint | Description |
|--------|----------|-------------|
| **Health & Status** | | |
| GET | `/api/v1/health` | Health check |
| GET | `/api/v1/status` | Daemon status (uptime, transfers, peers) |
| **Models** | | |
| GET | `/api/v1/models` | List local models |
| GET | `/api/v1/models/:name` | Get specific model details |
| POST | `/api/v1/models/download` | Download a model from P2P network |
| POST | `/api/v1/models/share` | Share a model on P2P network |
| DELETE | `/api/v1/models/:name` | Remove a model |
| **Discovery** | | |
| GET | `/api/v1/discover?pattern=<search>` | Discover models via BEP44 DHT |
| **Transfers** | | |
| GET | `/api/v1/transfers` | List active transfers |
| GET | `/api/v1/transfers/:id` | Get transfer details |
| PUT | `/api/v1/transfers/:id/pause` | Pause a transfer |
| PUT | `/api/v1/transfers/:id/resume` | Resume a transfer |
| DELETE | `/api/v1/transfers/:id` | Cancel a transfer |
| **Admin** | | |
| POST | `/api/v1/admin/shutdown` | Shutdown daemon |

### Using the API Directly

You can interact with the daemon directly using curl or any HTTP client:

```bash
# Check daemon health
curl http://localhost:8737/api/v1/health

# List models
curl http://localhost:8737/api/v1/models

# Discover models with "llama" in the name
curl "http://localhost:8737/api/v1/discover?pattern=llama"

# Get daemon status
curl http://localhost:8737/api/v1/status
```

### Remote Daemon

You can connect to a daemon running on another machine:

```bash
# Set the daemon URL
export SILMARIL_DAEMON_URL=http://remote-host:8737

# Now all CLI commands will use the remote daemon
silmaril list
silmaril discover
```


## Configuration

Silmaril uses a configuration file at `~/.config/silmaril/config.yaml` it will auto generate with defaults on first init. A complete example is provided in `config.yaml.example`.

Key configuration options:

```yaml
storage:
  base_dir: ~/.silmaril  # Base directory for all data
  
network:
  dht_enabled: true       # Enable DHT for decentralized discovery
  listen_port: 0          # 0 = random port (recommended)
  max_connections: 100    # Maximum peer connections
  disable_trackers: true  # Use DHT instead of trackers
  
daemon:
  bind_address: 0.0.0.0   # Bind to all interfaces (needed for Docker)
  port: 8737              # REST API port
  auto_start: true        # Auto-start daemon when needed
  
torrent:
  piece_length: 4194304   # 4MB pieces for optimal performance
  download_timeout: 0     # 0 = unlimited
  
security:
  verify_manifests: true  # Verify model signatures
  sign_manifests: true    # Sign shared models
```

## Architecture

### Daemon/Client Architecture

Silmaril uses a daemon/client architecture for efficient P2P operations:

- **Daemon**: A persistent background process that manages all P2P operations, DHT connections, and torrents
- **CLI Client**: A lightweight client that communicates with the daemon via REST API

### Core Technologies

- **BitTorrent v2** for file distribution
- **BEP 44 (DHT Mutable Items)** for decentralized model discovery without central trackers
- **DHT (Distributed Hash Table)** for decentralized peer discovery
- **Model manifests** for metadata and integrity verification
- **Dynamic registry** that scans your models directory at startup
- **REST API** for daemon/client communication

### Discovery System: Catalog-as-Torrent

Silmaril uses a "catalog-as-torrent" approach for scalable, decentralized model discovery:

#### How It Works

1. **Catalog Torrent**: The complete model catalog is stored as a JSON file distributed via BitTorrent
2. **BEP 44 Reference**: A small reference (84 bytes) containing the catalog torrent's infohash is stored in DHT using BEP 44
3. **Well-known Key**: All peers use the same DHT key (`silmaril-discovery-v1`) to find the catalog reference
4. **Smart Caching**: Peers cache the catalog locally and only re-download when the infohash changes

#### Discovery Flow

When you search for models:
1. Query the well-known DHT key to get the current catalog torrent infohash
2. Check if you already have this catalog version cached
3. If not, download the catalog torrent (usually just a few KB)
4. Search the local catalog for matching models
5. Download desired models directly via their individual torrents

#### Sharing Flow

When you share a model:
1. Your model is added to your local catalog
2. A new catalog torrent is created and seeded
3. The catalog reference in DHT is updated with the new infohash
4. Other peers will discover your update and can merge it with their catalogs

#### Benefits

- **Unlimited Scale**: No 1000-byte DHT limit - catalog can contain thousands of models
- **Efficient**: Only download catalog when it changes, not on every search
- **Resilient**: Multiple peers seed the catalog for redundancy
- **Decentralized**: No central server or tracker required
- **Automatic Refresh**: Daemon periodically refreshes catalog entries for seeded models

## Model Storage Structure

Models are stored in a HuggingFace-compatible structure:

```
~/.silmaril/models/
└── some-org/
    └── Model-v1-8B/
        ├── config.json
        ├── tokenizer.json
        ├── model-*.safetensors
        └── .silmaril.json  # Silmaril metadata
```


## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details