# Prepaid User Guide

This guide describes the flow for a DoubleZero user that prepays 2Z tokens to
connect to the network.

## Process

1. Invoke the initialize-prepaid-connection instruction. With your 2Z token
   account, you pay an activation fee to establish a new prepaid connection
   account. **NOTE: You have not yet paid for service yet. This activation fee
   effectively pays to reserve an IP on the network.**
2. Invoke the load-prepaid-connection instruction. With your 2Z token account,
   you specify how long you want service for in DoubleZero epochs (each roughly
   2 days long). With your 2Z token account, you pay for service per DoubleZero
   epoch.
3. On the DoubleZero Ledger network, the specified user will be airdropped gas
   to interact with the network. Please see the DoubleZero CLI for more
   information.

Details of each step of this process are outlined below.

### Initialize Prepaid Connection

When initializing the prepaid connection on Solana, you are specifying the user
pubkey of the account that will be interacting with the DoubleZero Ledger
network. You do not sign with this user keypair or need any SOL on this account.

This initialize instruction is permissionless. Anyone can pay to initialize the
prepaid connection on behalf of a DoubleZero Ledger network user.

This user will not have any gas on the DoubleZero Ledger network until the
prepaid connection account has service paid for on Solana, which only happens
after the prepaid connection is loaded for the desired service duration.

There is a specified activation fee (denominated in 2Z) in order to create this
prepaid connection account. The fee acts as a one-time payment to express intent
to pay for service. The new prepaid connection account will remain alive for as
long as it is unfunded or until the service expires.

Please be careful about letting your service expire. If it does, this prepaid
connection account runs the risk of being terminated. If this event happens, you
will have to initialize the prepaid connection by paying the activation fee
again.

You can find this fee amount on the Journal account, which specifies all prepaid
connection parameters on the Reward Distribution program.

### Load Prepaid Connection

Once you initialize the prepaid connection, it is now ready to load with 2Z to
prepay for service on the DoubleZero network. The service fees collected for
each DoubleZero epoch will be diverted to network contributors when The
Accountant computes rewards for each of these epochs.

Before attempting to load the prepaid connection, please review the prepaid
connection parameters on the Journal account. The following parameters should
help calculate the total service fee when loading a user account.

- Cost per epoch. This value specifies how much 2Z it costs to have service for
  each DoubleZero epoch. For example, if you establish service valid through
  DoubleZero epoch 10 and it is currently epoch 5, you will pay this cost
  multiplied by 6 epochs.
- Minimum epochs for service. When establishing service, you will have to pay
  for a minimum service term (in DoubleZero epochs). The instruction will revert
  if the specified DoubleZero epoch is less than the minimum.
- Maximum epochs for service. The program will restrict the length of service by
  this parameter. The instruction will revert if the specified DoubleZero epoch
  exceeds this maximum.

### Gas Delivery on DoubleZero Ledger

An off-chain oracle that tracks prepaid connections will send enough LedgerSOL
on the DoubleZero Ledger network to the specified user. The amount will be
enough to create a user account, which will enable to user to connect to the
DoubleZero network.

Please refer to the DoubleZero Serviceability program documentation for more
information.

## FAQ

### How do I keep my service in good standing?

Based on how long you pay for service, you can track the passage of DoubleZero
epochs to know how close your connection is close to expiration.

There are two ways to track these epochs:

- Fetch the current epoch via DoubleZero Ledger RPC via the [getEpochInfo]
  request.
- Fetch the next DoubleZero epoch from the Revenue Distribution program’s config
  account.

### What if my service lapses?

If the prepaid connection account indicates that the current DoubleZero epoch is
further than the epoch that this account is valid for, this account runs the
risk of being terminated.

Terminating a prepaid connection on Solana is a permissionless instruction,
where the caller is rewarded to call this instruction on delinquent accounts.

If the prepaid connection is terminated, it will need to be initialized again by
paying the activation fee again.

[getEpochInfo]: https://solana.com/docs/rpc/http/getepochinfo
