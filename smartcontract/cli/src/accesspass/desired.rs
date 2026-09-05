//! The access-pass definition document — the desired state `plan` and `apply` reconcile against.
//!
//! One entry per access pass, keyed by `(client_ip, user_payer)` — the pass's PDA seeds — and
//! naming the multicast groups it may publish to and subscribe to, plus its IBRL (unicast) tenant.
//! The schema matches the declaration an operator already writes in configuration management, so
//! both describe a host the same way:
//!
//! ```yaml
//! defaults:
//!   user_payer: 3UrShLQz2Y9UEaz69QhbZ41px91JYFSWd4hEs33ag3se
//!
//! access_passes:
//!   - client_ip: 203.0.113.10
//!     multicast:
//!       publish:
//!         - mg-marketdata-tob
//!       subscribe:
//!         - mg-marketdata-mbp
//!
//!   - client_ip: 203.0.113.11
//!     user_payer: AB3gAfgVBtb3AoJ2GwRGCuzCSWXit4isKLYm3kULWuf7
//!     ibrl: solana
//!     multicast:
//!       subscribe:
//!         - mg-analytics-mbp
//! ```
//!
//! **Every field is declarative**: a group the document does not name is revoked from that pass,
//! and an entry with no `ibrl` has its tenant cleared. An entry with no `multicast` block declares
//! no groups, and so revokes all of them.
//!
//! `ibrl` is a scalar because a pass admits one tenant and `access-pass set` is the only
//! instruction that writes `tenant_allowlist` — setting it is inherently a replace, so a list
//! would be misleading.
//!
//! Unknown keys are rejected, and the payer is resolved separately from parsing so a document can
//! be validated without a keypair or a network connection.

use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashSet, net::Ipv4Addr, path::Path, str::FromStr};

// `deny_unknown_fields` on every type here is load-bearing rather than tidiness: each optional
// field is read with a default, so `subscibe:` would otherwise parse as valid YAML, contribute
// nothing, and leave the host quietly unsubscribed.
#[derive(Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct AccessPassDocument {
    #[serde(default)]
    pub defaults: Defaults,
    #[serde(default)]
    pub access_passes: Vec<AccessPassEntry>,
}

#[derive(Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Defaults {
    /// Applied to any entry that omits `user_payer`. A fleet shares one payer, so this is
    /// usually the only place it appears.
    #[serde(default)]
    pub user_payer: Option<String>,
}

#[derive(Debug, Deserialize, Serialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct AccessPassEntry {
    pub client_ip: Ipv4Addr,
    #[serde(default)]
    pub user_payer: Option<String>,
    /// Tenant code granting IBRL (unicast) access. One code, or omitted for none.
    #[serde(default)]
    pub ibrl: Option<String>,
    #[serde(default)]
    pub multicast: Multicast,
}

#[derive(Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Multicast {
    #[serde(default)]
    pub publish: Vec<String>,
    #[serde(default)]
    pub subscribe: Vec<String>,
}

/// One document entry with its payer resolved and its group lists deduplicated.
#[derive(Debug, Clone, PartialEq)]
pub struct DesiredAccessPass {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub ibrl: Option<String>,
    pub publish: Vec<String>,
    pub subscribe: Vec<String>,
}

impl AccessPassDocument {
    pub fn from_path(path: &Path) -> eyre::Result<Self> {
        let raw = std::fs::read_to_string(path)
            .map_err(|e| eyre::eyre!("could not read {}: {e}", path.display()))?;
        Self::from_yaml(&raw)
    }

    pub fn from_yaml(raw: &str) -> eyre::Result<Self> {
        serde_yaml::from_str(raw).map_err(|e| eyre::eyre!("invalid access-pass document: {e}"))
    }

    /// Resolves every entry's payer and normalizes its group lists.
    ///
    /// `payer` is the current signer, used for the literal `"me"`. Resolution happens here rather
    /// than at parse time so the document stays a pure value that can be validated without a
    /// keypair or a network connection.
    pub fn resolve(&self, payer: Pubkey) -> eyre::Result<Vec<DesiredAccessPass>> {
        let mut resolved = Vec::with_capacity(self.access_passes.len());
        let mut seen: HashSet<(Ipv4Addr, Pubkey)> = HashSet::new();

        for (index, entry) in self.access_passes.iter().enumerate() {
            let raw_payer = entry
                .user_payer
                .as_deref()
                .or(self.defaults.user_payer.as_deref())
                .ok_or_else(|| {
                    eyre::eyre!(
                        "access_passes[{index}] ({}) has no user_payer and defaults.user_payer is unset",
                        entry.client_ip
                    )
                })?;

            let user_payer = if raw_payer.eq_ignore_ascii_case("me") {
                payer
            } else {
                Pubkey::from_str(raw_payer).map_err(|_| {
                    eyre::eyre!(
                        "access_passes[{index}] ({}) has an invalid user_payer: {raw_payer}",
                        entry.client_ip
                    )
                })?
            };

            // The pass is a PDA of (client_ip, user_payer), so two entries naming the same pair
            // describe the same account. Declarative lists make that a contradiction rather than
            // a merge: whichever entry ran last would revoke the other's groups.
            if !seen.insert((entry.client_ip, user_payer)) {
                eyre::bail!(
                    "access_passes[{index}]: {} / {user_payer} is declared more than once",
                    entry.client_ip
                );
            }

            // An empty string is a declaration mistake rather than "no tenant": omitting the
            // key is how you say that, and a blank value would otherwise resolve to no tenant and
            // silently revoke unicast access.
            if entry.ibrl.as_deref().is_some_and(|t| t.trim().is_empty()) {
                eyre::bail!(
                    "access_passes[{index}] ({}) has an empty ibrl; omit the key to declare no tenant",
                    entry.client_ip
                );
            }

            resolved.push(DesiredAccessPass {
                client_ip: entry.client_ip,
                user_payer,
                ibrl: entry.ibrl.clone(),
                publish: dedupe(&entry.multicast.publish),
                subscribe: dedupe(&entry.multicast.subscribe),
            });
        }

        Ok(resolved)
    }
}

fn dedupe(codes: &[String]) -> Vec<String> {
    let mut seen = HashSet::new();
    codes
        .iter()
        .filter(|code| seen.insert(code.as_str()))
        .cloned()
        .collect()
}

#[cfg(test)]
mod tests {
    use super::{AccessPassDocument, DesiredAccessPass};
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn parses_a_document_and_applies_the_default_payer() {
        let payer = Pubkey::new_unique();
        let other = Pubkey::new_unique();
        let doc = AccessPassDocument::from_yaml(&format!(
            r#"
defaults:
  user_payer: {payer}

access_passes:
  - client_ip: 203.0.113.10
    multicast:
      publish:
        - perps-tob
      subscribe:
        - perps-mbp
  - client_ip: 203.0.113.11
    user_payer: {other}
    multicast:
      subscribe:
        - pol-mbp
"#
        ))
        .unwrap();

        assert_eq!(
            doc.resolve(Pubkey::new_unique()).unwrap(),
            vec![
                DesiredAccessPass {
                    client_ip: [203, 0, 113, 10].into(),
                    user_payer: payer,
                    ibrl: None,
                    publish: vec!["perps-tob".to_string()],
                    subscribe: vec!["perps-mbp".to_string()],
                },
                DesiredAccessPass {
                    client_ip: [203, 0, 113, 11].into(),
                    user_payer: other,
                    ibrl: None,
                    publish: vec![],
                    subscribe: vec!["pol-mbp".to_string()],
                },
            ]
        );
    }

    #[test]
    fn resolves_me_to_the_current_payer() {
        let payer = Pubkey::new_unique();
        let doc = AccessPassDocument::from_yaml(
            r#"
access_passes:
  - client_ip: 203.0.113.10
    user_payer: me
    multicast:
      subscribe: [pol-mbp]
"#,
        )
        .unwrap();

        assert_eq!(doc.resolve(payer).unwrap()[0].user_payer, payer);
    }

    #[test]
    fn an_entry_with_no_multicast_block_declares_no_groups() {
        // Not a parse error: it is a legitimate declaration that revokes everything, and the
        // revocations show up in the plan for review before anything is sent.
        let payer = Pubkey::new_unique();
        let doc = AccessPassDocument::from_yaml(&format!(
            "defaults:\n  user_payer: {payer}\naccess_passes:\n  - client_ip: 203.0.113.10\n"
        ))
        .unwrap();

        let resolved = doc.resolve(payer).unwrap();
        assert!(resolved[0].publish.is_empty());
        assert!(resolved[0].subscribe.is_empty());
    }

    #[test]
    fn rejects_a_misspelled_key() {
        // The whole point of deny_unknown_fields: `subscibe` would otherwise default to empty and
        // silently unsubscribe the host.
        let err = AccessPassDocument::from_yaml(
            r#"
access_passes:
  - client_ip: 203.0.113.10
    user_payer: me
    multicast:
      subscibe: [pol-mbp]
"#,
        )
        .unwrap_err();
        assert!(err.to_string().contains("subscibe"), "{err}");
    }

    #[test]
    fn rejects_an_unknown_top_level_key() {
        let err = AccessPassDocument::from_yaml("acces_passes: []").unwrap_err();
        assert!(err.to_string().contains("acces_passes"), "{err}");
    }

    #[test]
    fn rejects_an_entry_with_no_payer_anywhere() {
        let doc =
            AccessPassDocument::from_yaml("access_passes:\n  - client_ip: 203.0.113.10\n").unwrap();
        let err = doc.resolve(Pubkey::new_unique()).unwrap_err();
        assert!(err.to_string().contains("no user_payer"), "{err}");
    }

    #[test]
    fn rejects_a_malformed_payer() {
        let doc = AccessPassDocument::from_yaml(
            "access_passes:\n  - client_ip: 203.0.113.10\n    user_payer: not_a_pubkey\n",
        )
        .unwrap();
        let err = doc.resolve(Pubkey::new_unique()).unwrap_err();
        assert!(err.to_string().contains("invalid user_payer"), "{err}");
    }

    #[test]
    fn rejects_a_malformed_client_ip() {
        let err = AccessPassDocument::from_yaml(
            "access_passes:\n  - client_ip: 203.0.113\n    user_payer: me\n",
        )
        .unwrap_err();
        assert!(err.to_string().contains("client_ip"), "{err}");
    }

    #[test]
    fn rejects_the_same_pass_declared_twice() {
        let payer = Pubkey::new_unique();
        let doc = AccessPassDocument::from_yaml(&format!(
            r#"
defaults:
  user_payer: {payer}
access_passes:
  - client_ip: 203.0.113.10
    multicast:
      subscribe: [a]
  - client_ip: 203.0.113.10
    multicast:
      subscribe: [b]
"#
        ))
        .unwrap();
        let err = doc.resolve(payer).unwrap_err();
        assert!(err.to_string().contains("declared more than once"), "{err}");
    }

    #[test]
    fn deduplicates_repeated_group_codes() {
        let payer = Pubkey::new_unique();
        let doc = AccessPassDocument::from_yaml(&format!(
            "defaults:\n  user_payer: {payer}\naccess_passes:\n  - client_ip: 203.0.113.10\n    multicast:\n      subscribe: [a, a, b]\n"
        ))
        .unwrap();
        assert_eq!(
            doc.resolve(payer).unwrap()[0].subscribe,
            vec!["a".to_string(), "b".to_string()]
        );
    }
}
