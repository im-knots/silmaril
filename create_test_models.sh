#!/bin/bash

# Create different test models with unique content for testing

MODELS_DIR="test/models/test-org"

# Create a few test models with different content
for name in model-alpha model-beta model-gamma; do
    MODEL_DIR="$MODELS_DIR/$name"
    
    echo "Creating test model: $name"
    mkdir -p "$MODEL_DIR"
    
    # Create unique content for each model
    echo "This is test model: $name" > "$MODEL_DIR/README.md"
    echo "Created at: $(date)" >> "$MODEL_DIR/README.md"
    echo "Random seed: $RANDOM" >> "$MODEL_DIR/README.md"
    
    # Create a config file with unique content
    cat > "$MODEL_DIR/config.json" <<EOF
{
  "name": "$name",
  "model_type": "test",
  "architectures": ["TestModel"],
  "num_parameters": $((RANDOM % 10000)),
  "hidden_size": $((128 + RANDOM % 256)),
  "num_hidden_layers": $((2 + RANDOM % 10)),
  "random_seed": $RANDOM
}
EOF
    
    # Create a dummy weights file with random data (small)
    dd if=/dev/urandom of="$MODEL_DIR/model.bin" bs=1024 count=$((100 + RANDOM % 400)) 2>/dev/null
    
    echo "Created $name with unique content"
done

echo "Test models created successfully!"
ls -la $MODELS_DIR/