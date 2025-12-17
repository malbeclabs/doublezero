# Changelog

## Unreleased

- debt write off activation ([#99])

## [v0.2.0]

- add null rewards root protection ([#86])
- fix swap balance in journal ([#87])
- allow same-distribution debt write-offs ([#91])
- update dependencies ([#92])
- track debt write-offs in state ([#93])
- update Solana crates to v3 ([#94])
- uptick version to 0.2.0 ([#95])

## [v0.1.1]

- uptick svm-hash ([#83])
- fix CPI seeds for sweep ([#84])

## [v0.1.0] 

- add reward distribution program ([#1])
- add prepaid user handling ([#2])
- add development feature ([#3])
- clean up rust deps ([#7])
- add contributor rewards ([#8])
- expand Solana validator fee parameters ([#9])
- split accountant key and add finalize instruction args ([#10])
- add sentinel to grant/deny prepaid access ([#11])
- split initialize-contributor-rewards instruction ([#13])
- split distribution configuration ([#17])
- add verify distribution merkle root ([#19])
- add migrate instruction ([#23])
- uptick msrv to 1.84 and solana version 2.3.7 ([#32])
- add space for payments and claims ([#37])
- pay Solana vlaidator debt ([#40])
- fix mainnet 2Z key ([#42])
- add reward distribution precursor instructions ([#45])
- fixed SOL fee and better debt handling ([#46])
- add economic burn rate encoding ([#47])
- block setting a new rewards manager ([#48])
- add withdraw SOL scaffolding ([#49])
- distribute rewards ([#50])
- onchain clean up ([#52])
- enforce grace period after initializing distribution ([#53])
- enforce distribution sweeping in epoch orer ([#54])
- fix Solana validator deposit account creation ([#55])
- separate versions for programs ([#66])
- remove prepaid handling ([#69])
- recipient shares cannot be zero ([#75])
- fix dequeue fills cpi account meta ([#76])
- require recipients be configured ([#77])
- add initialize distribution grace period ([#78])
- add revert if distribution count == total ([#79])
- next_dz_epoch -> next_completed_dz_epoch ([#80])
- fix `try_initialize` ([#81])

[#1]: https://github.com/doublezerofoundation/doublezero-solana/pull/1
[#2]: https://github.com/doublezerofoundation/doublezero-solana/pull/2
[#3]: https://github.com/doublezerofoundation/doublezero-solana/pull/3
[#7]: https://github.com/doublezerofoundation/doublezero-solana/pull/7
[#8]: https://github.com/doublezerofoundation/doublezero-solana/pull/8
[#9]: https://github.com/doublezerofoundation/doublezero-solana/pull/9
[#10]: https://github.com/doublezerofoundation/doublezero-solana/pull/10
[#11]: https://github.com/doublezerofoundation/doublezero-solana/pull/11
[#13]: https://github.com/doublezerofoundation/doublezero-solana/pull/13
[#17]: https://github.com/doublezerofoundation/doublezero-solana/pull/17
[#19]: https://github.com/doublezerofoundation/doublezero-solana/pull/19
[#23]: https://github.com/doublezerofoundation/doublezero-solana/pull/23
[#32]: https://github.com/doublezerofoundation/doublezero-solana/pull/32
[#37]: https://github.com/doublezerofoundation/doublezero-solana/pull/37
[#40]: https://github.com/doublezerofoundation/doublezero-solana/pull/40
[#42]: https://github.com/doublezerofoundation/doublezero-solana/pull/42
[#45]: https://github.com/doublezerofoundation/doublezero-solana/pull/45
[#46]: https://github.com/doublezerofoundation/doublezero-solana/pull/46
[#47]: https://github.com/doublezerofoundation/doublezero-solana/pull/47
[#48]: https://github.com/doublezerofoundation/doublezero-solana/pull/48
[#49]: https://github.com/doublezerofoundation/doublezero-solana/pull/49
[#50]: https://github.com/doublezerofoundation/doublezero-solana/pull/50
[#52]: https://github.com/doublezerofoundation/doublezero-solana/pull/52
[#53]: https://github.com/doublezerofoundation/doublezero-solana/pull/53
[#54]: https://github.com/doublezerofoundation/doublezero-solana/pull/54
[#55]: https://github.com/doublezerofoundation/doublezero-solana/pull/55
[#66]: https://github.com/doublezerofoundation/doublezero-solana/pull/66
[#69]: https://github.com/doublezerofoundation/doublezero-solana/pull/69
[#75]: https://github.com/doublezerofoundation/doublezero-solana/pull/75
[#76]: https://github.com/doublezerofoundation/doublezero-solana/pull/76
[#77]: https://github.com/doublezerofoundation/doublezero-solana/pull/77
[#78]: https://github.com/doublezerofoundation/doublezero-solana/pull/78
[#79]: https://github.com/doublezerofoundation/doublezero-solana/pull/79
[#80]: https://github.com/doublezerofoundation/doublezero-solana/pull/80
[#81]: https://github.com/doublezerofoundation/doublezero-solana/pull/81
[#83]: https://github.com/doublezerofoundation/doublezero-solana/pull/83
[#84]: https://github.com/doublezerofoundation/doublezero-solana/pull/84
[#86]: https://github.com/doublezerofoundation/doublezero-solana/pull/86
[#87]: https://github.com/doublezerofoundation/doublezero-solana/pull/87
[#91]: https://github.com/doublezerofoundation/doublezero-solana/pull/91
[#92]: https://github.com/doublezerofoundation/doublezero-solana/pull/92
[#93]: https://github.com/doublezerofoundation/doublezero-solana/pull/93
[#94]: https://github.com/doublezerofoundation/doublezero-solana/pull/94
[#95]: https://github.com/doublezerofoundation/doublezero-solana/pull/95
[#99]: https://github.com/doublezerofoundation/doublezero-solana/pull/99
[v0.1.0]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.1.0
[v0.1.1]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.1.1
[v0.2.0]: https://github.com/doublezerofoundation/doublezero-solana/tree/revenue-distribution/v0.2.0
