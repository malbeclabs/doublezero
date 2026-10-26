use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_feed_pda, processors::feed::create::FeedCreateArgs, state::feed::FeedChain,
};
use doublezero_serviceability_instruction::feed::create_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateFeedCommand {
    pub code: String,
    pub name: String,
    /// The metro (exchange) this feed serves; part of the PDA seed.
    pub exchange: Pubkey,
    /// Multicast groups joinable in this metro.
    pub groups: Vec<Pubkey>,
    pub feed_chain: FeedChain,
    /// The RFC-28 terms that make this a bonded feed. `None` creates a catalog feed.
    pub bond: Option<FeedBondTerms>,
}

/// FeedBondTerms are the terms a bonded feed carries. They travel together: the program refuses a
/// feed with some of them and not the others, so the command takes all of them or none.
#[derive(Debug, PartialEq, Clone)]
pub struct FeedBondTerms {
    /// The builder deploying the feed. The bond admits the create only when this is the payer;
    /// an admin creating the feed on a builder's behalf names the builder here.
    pub builder: Pubkey,
    /// The `BuilderBond` account on Solana behind the feed.
    pub bond_ref: Pubkey,
    /// The `edge-feed-spec` wire format, as `<spec>@<version>`.
    pub spec_id: String,
    /// SHA-256 of the declared service level, or all zeros to declare none.
    pub sla_hash: [u8; 32],
    /// The rate the feed commits to, which the bond's tier must cover.
    pub committed_rate_bits_per_sec: u64,
}

impl CreateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (pda_pubkey, _) = get_feed_pda(&program_id, &code, &self.exchange);

        let mut args = FeedCreateArgs {
            code,
            name: self.name.clone(),
            exchange: self.exchange,
            groups: self.groups.clone(),
            feed_chain: self.feed_chain,
            ..Default::default()
        };
        if let Some(bond) = &self.bond {
            args.builder = bond.builder;
            args.bond_ref = bond.bond_ref;
            args.spec_id = bond.spec_id.clone();
            args.sla_hash = bond.sla_hash;
            args.committed_rate_bits_per_sec = bond.committed_rate_bits_per_sec;
        }
        let ix = create_feed(&program_id, &client.get_payer(), args);

        client.send_transaction(ix).map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::create::{CreateFeedCommand, FeedBondTerms},
        tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        processors::feed::create::FeedCreateArgs, state::feed::FeedChain,
    };
    use doublezero_serviceability_instruction::feed::create_feed;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_feed_create_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let exchange = Pubkey::new_unique();
        let group = Pubkey::new_unique();

        let expected = create_feed(
            &program_id,
            &payer,
            FeedCreateArgs {
                code: "test_feed".to_string(),
                name: "Test Feed".to_string(),
                exchange,
                groups: vec![group],
                feed_chain: FeedChain::Solana,
                ..Default::default()
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let create_command = CreateFeedCommand {
            code: "test_feed".to_string(),
            name: "Test Feed".to_string(),
            exchange,
            groups: vec![group],
            feed_chain: FeedChain::Solana,
            bond: None,
        };

        let create_invalid_command = CreateFeedCommand {
            code: "test/feed".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }

    #[test]
    fn test_commands_feed_create_carries_the_bond_terms() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let exchange = Pubkey::new_unique();
        let group = Pubkey::new_unique();
        let bond_ref = Pubkey::new_unique();

        // The full args, so a term the command drops fails the match rather than defaulting.
        let expected = create_feed(
            &program_id,
            &payer,
            FeedCreateArgs {
                code: "bonded".to_string(),
                name: "Bonded".to_string(),
                exchange,
                groups: vec![group],
                builder: payer,
                bond_ref,
                spec_id: "top-of-book@v1.0.0".to_string(),
                sla_hash: [9u8; 32],
                committed_rate_bits_per_sec: 1_000_000_000,
                feed_chain: FeedChain::Unspecified,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateFeedCommand {
            code: "bonded".to_string(),
            name: "Bonded".to_string(),
            exchange,
            groups: vec![group],
            feed_chain: FeedChain::Unspecified,
            bond: Some(FeedBondTerms {
                builder: payer,
                bond_ref,
                spec_id: "top-of-book@v1.0.0".to_string(),
                sla_hash: [9u8; 32],
                committed_rate_bits_per_sec: 1_000_000_000,
            }),
        }
        .execute(&client);
        assert!(res.is_ok(), "{res:?}");
    }
}
