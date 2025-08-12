# Silmaril - P2P AI Model Distribution

Silmaril is a peer-to-peer distribution system for AI models using BitTorrent. It enables efficient downloading and sharing of models, metadata, and datasets across the bittorrent network.

## Features

- **P2P Distribution**: Share models using BitTorrent protocol with DHT discovery
- **HuggingFace Compatible**: Works with models in HuggingFace format
- **Dynamic Registry**: Automatically discovers and manages models in your local directory
- **Git Integration**: Mirror models directly from HuggingFace repositories


## Installation

### From Source

```bash
git clone https://github.com/yourusername/silmaril.git
cd silmaril
make build
```

The binary will be available at `./silmaril`.

## Quick Start

### 0. Initialize Silmaril

Set up the required directories and configuration:

```bash
silmaril init
```

This will create:
- `~/.silmaril/` directory structure for models, torrents, and metadata
- `~/.config/silmaril/config.yaml` with default settings

To initialize in a custom location:

```bash
silmaril init --path /path/to/custom/location
```

To completely remove Silmaril and all downloaded models:

```bash
silmaril init --cleanup
```

### 1. Discover Available Models

Search for models shared by other users on the P2P network:

```bash
silmaril discover
```

Search for a specific model:

```bash
silmaril discover llama
```

### 2. Download a Model

Download a model from the P2P network:

```bash
silmaril get meta-llama/Llama-3.1-8B
```

The model will be downloaded to `~/.silmaril/models/` by default.

### 3. Mirror from HuggingFace

Clone a model directly from HuggingFace and automatically share it on the P2P network:

```bash
silmaril mirror https://huggingface.co/mistralai/Mistral-7B-v0.1
# or simply:
silmaril mirror mistralai/Mistral-7B-v0.1
```

The model will be automatically seeded after mirroring. Use `--no-auto-share` to disable this.

### 4. Share Your Models

Seed all your downloaded models to help others:

```bash
silmaril share --all
```

Share a specific model by name:

```bash
silmaril share meta-llama/Llama-3.1-8B
```

Or share/publish a new model from a directory:

```bash
silmaril share /path/to/model --name org/model --license apache-2.0
```

### 5. List Local Models

See what models you have downloaded:

```bash
silmaril list
```

## Publishing Your Own Models

If you have a model in HuggingFace format, you can publish it to the network using the share command:

```bash
silmaril share /path/to/your/model \
  --name "yourorg/yourmodel" \
  --license "apache-2.0" \
  --version "v1.0"
```

This will:
1. Create a torrent file for P2P distribution
2. Generate a manifest with metadata
3. Announce the model on the DHT network
4. Save to your local registry
5. Start seeding the model immediately

## Configuration

Silmaril uses a configuration file at `~/.config/silmaril/config.yaml`. Here's an example:

```yaml
storage:
  base_dir: ~/.silmaril
  models_dir: ~/.silmaril/models

network:
  max_connections: 50
  upload_rate_limit: 0  # 0 = unlimited
  download_rate_limit: 0
  dht_enabled: true
  dht_port: 0  # 0 = random port (allows multiple instances)
  listen_port: 0  # 0 = random port

torrent:
  piece_length: 4194304  # 4MB
  download_timeout: 3600  # 1 hour

security:
  verify_checksums: true
  sign_manifests: true
```

## Commands Reference

### Core Commands

- `silmaril init` - Initialize Silmaril directories and configuration
- `silmaril list` - List locally downloaded models
- `silmaril discover [model]` - Search for models on the P2P network
- `silmaril get <model>` - Download a model from the network
- `silmaril share [model/path]` - Share existing models or publish new ones
- `silmaril mirror <repo>` - Clone from HuggingFace and auto-share via P2P

### Registry Management

- `silmaril registry import <manifest.json>` - Import a model manifest
- `silmaril registry export <model-name>` - Export a model manifest

### Command Options

#### init
- `--path` - Initialize in a custom location
- `--cleanup` - Remove all Silmaril directories and configuration
- `--force` - Overwrite existing configuration

#### get
- `--output, -o` - Specify output directory
- `--seed` - Continue seeding after download (default: true)
- `--no-verify` - Skip checksum verification

#### mirror
- `--branch` - Git branch to clone (default: main)
- `--depth` - Git clone depth (default: 1)
- `--skip-lfs` - Skip Git LFS files
- `--no-broadcast` - Don't announce on DHT
- `--no-auto-share` - Don't automatically start sharing after mirroring

#### share
- `--all` - Share all downloaded models
- `--name` - Model name (required for publishing new models)
- `--license` - Model license (required for publishing new models)
- `--version` - Model version (default: main)
- `--piece-length` - Torrent piece size (default: 4MB)
- `--skip-dht` - Skip DHT announcement
- `--sign` - Sign the manifest (default: true)

## Architecture

Silmaril uses:
- **BitTorrent v2** for efficient file distribution
- **DHT (Distributed Hash Table)** for decentralized peer discovery
- **Model manifests** for metadata and integrity verification
- **Dynamic registry** that scans your models directory at startup
- **Random ports** by default to allow multiple commands to run simultaneously

## Model Storage Structure

Models are stored in a HuggingFace-compatible structure:

```
~/.silmaril/models/
└── meta-llama/
    └── Llama-3.1-8B/
        ├── config.json
        ├── tokenizer.json
        ├── model-*.safetensors
        └── .silmaril.json  # Silmaril metadata
```

## Tips & Best Practices

1. **Auto-sharing**: The `mirror` command automatically starts sharing after downloading. This helps build the P2P network.

2. **Publishing models**: Use `share` with `--name` and `--license` to publish any HuggingFace-format model directory:
   ```bash
   silmaril share ./my-model --name myorg/mymodel --license MIT
   ```

3. **Multiple instances**: Silmaril uses random ports by default, so you can run multiple commands simultaneously:
   ```bash
   # Terminal 1: Share all models
   silmaril share --all
   
   # Terminal 2: Discover new models (works simultaneously)
   silmaril discover
   ```

4. **Quick setup**: Initialize, mirror a model, and start sharing in one go:
   ```bash
   silmaril init
   silmaril mirror mistralai/Mistral-7B-v0.1
   # Model is automatically shared!
   ```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details