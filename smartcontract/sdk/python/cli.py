import argparse, asyncio, json, sys
from rpc_http import HttpRPCClient, _b58_to_32
from serviceability_borsh import Client, pretty_pubkey
from normalize import normalize, NORM_BY_TYPE

ENVS = {
  "mainnet-beta": {
    "rpc": "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab",
    "program_id": "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv",
  },
  "testnet": {
    "rpc": "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
    "program_id": "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb",
  },
  "devnet": {
    "rpc": "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
    "program_id": "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah",
  },
}

async def main():
  ap = argparse.ArgumentParser(prog="serviceability")
  ap.add_argument("type", choices=NORM_BY_TYPE.keys(), help="which account type to print")
  ap.add_argument("--env", default="mainnet-beta", choices=ENVS.keys(), help="which environment to use")
  ap.add_argument("--verbose", action="store_true", help="print parse errors to stderr")
  args = ap.parse_args()

  env = ENVS[args.env]
  rpc = HttpRPCClient(env["rpc"])
  program_id = _b58_to_32(env["program_id"])

  c = Client(rpc, program_id)
  pd = await c.get_program_data()

  getter, norm = NORM_BY_TYPE[args.type]
  raw = getter(pd)
  rows = [normalize(x, norm) for x in raw]

  # stdout: machine-readable only
  print(json.dumps(rows, indent=2))

  # stderr: diagnostics
  if args.verbose:
    print(f"\nparse_errors={len(pd.parse_errors)}", file=sys.stderr)
    for pk, t, n, msg in pd.parse_errors[:50]:
      print(
        f"- acct={pretty_pubkey(pk)} type_byte={t} len={n} err={msg}",
        file=sys.stderr,
      )

asyncio.run(main())
