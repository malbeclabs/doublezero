# Changelog

## Unreleased

- register rewards integrations (#113)
- simplify already-registered rejection (#114)
- scaffold integration harvesting (#115)
- collect integration rewards (#116)
- track collected integrations via inline bitmap on distribution (#117)

## [v0.3.4]
- withdraw deposited SOL (#111)

## [v0.3.3]

- set distribution economic burn rate (#109)

## [v0.3.2]

- fix zero-fee handling  (#107)

## [v0.3.1]

- handle direct 2Z payments to Journal's ATA (#106)

## [v0.3.0]

- fix initialize-distribution interface (#105)

## [v0.2.1]

- debt write off activation (#99)
- uptick version to 0.2.1 (#101)

## [v0.2.0]

- add null rewards root protection (#86)
- fix swap balance in journal (#87)
- allow same-distribution debt write-offs (#91)
- update dependencies (#92)
- track debt write-offs in state (#93)
- update Solana crates to v3 (#94)
- uptick version to 0.2.0 (#95)

## [v0.1.1]

- uptick svm-hash (#83)
- fix CPI seeds for sweep (#84)

## [v0.1.0] 

- add reward distribution program (#1)
- add prepaid user handling (#2)
- add development feature (#3)
- clean up rust deps (#7)
- add contributor rewards (#8)
- expand Solana validator fee parameters (#9)
- split accountant key and add finalize instruction args (#10)
- add sentinel to grant/deny prepaid access (#11)
- split initialize-contributor-rewards instruction (#13)
- split distribution configuration (#17)
- add verify distribution merkle root (#19)
- add migrate instruction (#23)
- uptick msrv to 1.84 and solana version 2.3.7 (#32)
- add space for payments and claims (#37)
- pay Solana vlaidator debt (#40)
- fix mainnet 2Z key (#42)
- add reward distribution precursor instructions (#45)
- fixed SOL fee and better debt handling (#46)
- add economic burn rate encoding (#47)
- block setting a new rewards manager (#48)
- add withdraw SOL scaffolding (#49)
- distribute rewards (#50)
- onchain clean up (#52)
- enforce grace period after initializing distribution (#53)
- enforce distribution sweeping in epoch orer (#54)
- fix Solana validator deposit account creation (#55)
- separate versions for programs (#66)
- remove prepaid handling (#69)
- recipient shares cannot be zero (#75)
- fix dequeue fills cpi account meta (#76)
- require recipients be configured (#77)
- add initialize distribution grace period (#78)
- add revert if distribution count == total (#79)
- next_dz_epoch -> next_completed_dz_epoch (#80)
- fix `try_initialize` (#81)

[v0.1.0]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.1.0
[v0.1.1]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.1.1
[v0.2.0]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.2.0
[v0.2.1]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.2.1
[v0.3.0]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.3.0
[v0.3.1]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.3.1
[v0.3.2]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.3.2
[v0.3.3]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.3.3
[v0.3.4]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.3.4
