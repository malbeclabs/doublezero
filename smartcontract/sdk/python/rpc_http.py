# rpc_http.py
from __future__ import annotations
from typing import List, Sequence, Tuple, Optional
import base64, json
import httpx

Bytes32 = bytes

def _b58_to_32(s: str) -> Bytes32:
  import base58
  b = base58.b58decode(s)
  if len(b) != 32: raise ValueError("pubkey not 32 bytes")
  return b

class HttpRPCClient:
  def __init__(self, rpc_url: str, timeout: float = 20.0):
    self.rpc_url = rpc_url
    self.timeout = timeout
    self._id = 1

  async def _rpc(self, method: str, params: list) -> dict:
    payload = {"jsonrpc":"2.0","id":self._id,"method":method,"params":params}
    self._id += 1
    async with httpx.AsyncClient(timeout=self.timeout) as c:
      r = await c.post(self.rpc_url, json=payload)
      r.raise_for_status()
      out = r.json()
    if "error" in out: raise RuntimeError(out["error"])
    return out["result"]

  async def get_program_accounts(self, program_id: Bytes32) -> Sequence[Tuple[Bytes32, bytes]]:
    import base58
    program_b58 = base58.b58encode(program_id).decode("ascii")
    # encoding=base64 so we can decode bytes without guesswork
    params = [
      program_b58,
      {
        "encoding": "base64",
        # You can add filters here later (dataSize/memcmp) if desired.
      },
    ]
    result = await self._rpc("getProgramAccounts", params)

    out: List[Tuple[Bytes32, bytes]] = []
    for item in result:
      pk = _b58_to_32(item["pubkey"])
      data0, enc = item["account"]["data"]
      if enc != "base64": raise RuntimeError(f"unexpected encoding {enc}")
      raw = base64.b64decode(data0)
      out.append((pk, raw))
    return out
