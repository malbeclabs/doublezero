from mnemonic import Mnemonic
import json

with open("./target/deploy/double_zero_sla_program-keypair.json", "r") as file:
    keypair = json.load(file)

private_key_bytes = bytes(keypair[:32])

mnemo = Mnemonic("english")
mnemonic_phrase = mnemo.to_mnemonic(private_key_bytes)

print("Frase mnemotécnica BIP-39:", mnemonic_phrase)

