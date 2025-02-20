import subprocess
import re

# Define the file paths
go_file_path = '../../sdk/go/client.go'
rust_file_path = '../../sdk/rs/src/client.rs'

# Function to execute a shell command and return the output
def execute_command(command):
    result = subprocess.run(command, shell=True, capture_output=True, text=True)
    return result.stdout.strip()

# Get the new pubkey by executing the provided command
command = 'solana address -k ./target/deploy/double_zero_sla_program-keypair.json'
pubkey = execute_command(command)

print(f"Pubkey: {pubkey}")

# Function to update pubkey in a file
def update_pubkey(file_path, pattern, replacement):
    with open(file_path, 'r') as file:
        content = file.read()

    updated_content = re.sub(pattern, replacement, content)

    with open(file_path, 'w') as file:
        file.write(updated_content)

# Update pubkey in Go file
go_pattern = r'const PROGRAM_ID = ".*?"'
go_replacement = f'const PROGRAM_ID = "{pubkey}"'
update_pubkey(go_file_path, go_pattern, go_replacement)

# Update pubkey in Rust file
rust_pattern = r'static PROGRAM_ID: &str = ".*?";'
rust_replacement = f'static PROGRAM_ID: &str = "{pubkey}";'
update_pubkey(rust_file_path, rust_pattern, rust_replacement)

print("Pubkey updated successfully in Go and Rust files.")