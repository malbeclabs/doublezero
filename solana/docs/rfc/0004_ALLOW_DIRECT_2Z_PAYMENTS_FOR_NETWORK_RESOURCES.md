# Allow Direct 2Z Payments for Network Resources

---

## Summary

This RFC introduces a canonical token account for collecting 2Z payments. It
does not define how payment amounts are determined or enforced.

For minimum viable products (MVPs) using a specific resource on the network
(e.g. multicast publishing), onchain enforcement of paying for these specific
products may not be defined at the time of product deployment on the physical
network. But in the meantime, there should be a mechanism to reward network
contributors with offchain processes to validate these 2Z payments.

This RFC exists to decouple resource availability from enforceable onchain
billing.

## Motivation

Currently, the protocol only supports one type of user: Solana validators paying
a proportion of their block rewards. In order to support other tenants more
easily, there should not be a coupling between resource access and billing
enforceable onchain.

If there were a way for other users to pay the protocol, where these payment
amounts would be determined offchain, network contributors can be rewarded
without any bottlenecks from engineering to build onchain solutions to support
collecting payments for specific use cases.

Because 2Z is how network contributors are rewarded, there should be a way to
accept direct 2Z payments to the protocol.

As the protocol refines the use cases it supports on the network, these direct
payments can shift to a more specific payment workflow as soon as it is defined
onchain. And while users migrate from one payment method to another, the end
result to network contributors should not change: they continue to be rewarded
for usage on the network.

It is important to note that this RFC does not aim to discuss any specific
offchain computation for enforcing payments. Any specific payment scheme for a
new network user should be a separate RFC.

## New Terminology

Direct 2Z payments is a new term introduced to the protocol. This type of
payment is neither a resource-scoped mechanism nor tied to any onchain
enforcement. These payments are aggregated into the latest distribution and are
subject to the same rewards deferral period.

## Alternatives Considered

As the DoubleZero network supports new use cases, there can always be an
engineering constraint to enforce the payment onchain tied to this use. But not
all use cases can be supported onchain easily. One such example is supporting a
non-crypto user, whose usage will have to be determined offchain in order for
the protocol to invoice this user.

Support to add data onchain that ties this usage to a particular payment amount
based on a specific fee structure may hurt network contributors if the protocol
does not have everything built to support the user fully (new features on the
physical network in addition to onchain payment support). Introducing these
delays is an inefficient way of onboarding new types of users.

## Detailed Design

### Onchain

The distribution account already supports a field
`collected_prepaid_2z_payments`, which can be used to aggregate all of the
direct 2Z payments to the protocol.

There should be a token account (specifically an Associated Token Account) that
will act as a deposit address for all direct 2Z payments to the protocol. The
owner will be the Journal PDA. It acts as a destination of 2Z aggregated among
all users without onchain payment enforcement, meaning that it does not encode
attribution to a specific network resource.

The initialize-distribution instruction should take the Journal’s ATA as an
additional account. This instruction processor should deserialize the token
account in order to transfer its full balance to the latest distribution’s token
account. This balance will be added to the `collected_prepaid_2z_payments`
field.

The smart contract already factors in this field when calculating the total 2Z
for reward distributions. Up until this proposal, this field was just zero.

Prior to upgrading the smart contract, the Revenue Distribution program
interface should be updated to add the additional Journal ATA account so that
the debt accountant offchain process can be ready for the onchain change of
using this account when it initializes new distributions. Until then, the extra
account will just be unused.

### Offchain

Because the ATA may not be created for the Journal yet, it will need to exist
before the initialize-distribution instruction can use this account. Creating
ATAs is permissionless, so anyone can invoke the create instruction.

The debt accountant offchain process should integrate with the new interface
prior to the onchain upgrade.

Because the Journal ATA acts as a deposit address for 2Z, payments can be made
directly by transferring 2Z to this token account (and can be easily derived
knowing that the Journal PDA is its owner). But there should be support in the
CLI to allow users to easily transfer 2Z to this account.

## Impact

The only onchain change is how the initialize-distribution instruction will
handle this new token account. Handling the transfer from this new account is a
minimal change.

The debt accountant change is also minimal since it only needs the latest
interface prior to an upgrade. It is crucial, however, for the updated debt
accountant to be deployed prior to the onchain upgrade. Otherwise, the debt
accountant process will not be able to initialize a new distribution until it
knows about the new interface.

By adding this new token account to accept 2Z payments, the protocol will be
able to enable more types of paying users ready to join the DoubleZero network
by leveraging offchain computation to validate these payments.

## Security Considerations

This change does not introduce new attack surfaces. But it is important to note
that this Journal ATA will be swept of its balance at the time a new
distribution is initialized. Any 2Z inadvertently sent to this account will be
taken to distribute to network contributors.

## Backward Compatibility

This change is technically a breaking change to the initialize-distribution
instruction. But if the interface is changed prior to the smart contract
implementing the change to handle this new token account, the rollout will not
impede the protocol from initializing new distributions.

The interface change only requires a minor version bump if a major version is
established or a patch version bump if zero-versioned. And the smart contract
implementation should bump the patch version.

## Open Questions

- Instead of sweeping the balance of the Journal ATA to the latest distribution,
  should it be swept to the about-to-be-rewarded distribution instead?
