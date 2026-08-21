use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, device::get::GetDeviceCommand,
        user::instructions_with_ip_proof,
    },
    DoubleZeroClient,
};
use doublezero_ip_proof::IpOwnershipProof;
use doublezero_serviceability::{
    pda::get_user_pda,
    processors::user::create::UserCreateArgs,
    state::user::{UserCYOA, UserType},
};
use doublezero_serviceability_instruction::user::create_user;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateUserCommand {
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: Ipv4Addr,
    pub tunnel_endpoint: Ipv4Addr,
    pub tenant_pk: Option<Pubkey>,
    /// RFC-27 proof that the payer originated a request from `client_ip`, obtained from the
    /// DoubleZero IP verification service. Supplying it also pulls the Instructions sysvar into
    /// the account list and a native `Ed25519SigVerify` instruction into the transaction; leaving
    /// it `None` produces the pre-RFC-27 shape, which the program accepts until
    /// `require-ip-ownership-proof` is set for the environment.
    pub ip_proof: Option<IpOwnershipProof>,
}

impl CreateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        // GetAccessPassCommand prefers a shared dynamic (UNSPECIFIED) pass and falls
        // back to the exact client-IP pass, matching the onchain create_user path.
        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: client.get_payer(),
        }
        .execute(client)?
        .ok_or_else(|| eyre::eyre!("You have no Access Pass"))?;

        let program_id = client.get_program_id();
        let (pda_pubkey, _) = get_user_pda(&program_id, &self.client_ip, self.user_type);

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.device_pk.to_string(),
        }
        .execute(client)
        .map_err(|_| eyre::eyre!("Device not found"))?;
        let dz_prefix_count = device.dz_prefixes.len();
        if dz_prefix_count == 0 {
            return Err(eyre::eyre!(
                "Device {} has no dz_prefixes; cannot create user",
                self.device_pk
            ));
        }
        let dz_prefix_count_u8 = u8::try_from(dz_prefix_count).map_err(|_| {
            eyre::eyre!(
                "Device {} has {} dz_prefixes, exceeds u8::MAX",
                self.device_pk,
                dz_prefix_count
            )
        })?;

        let ix = create_user(
            &program_id,
            &client.get_payer(),
            &self.device_pk,
            &accesspass_pk,
            dz_prefix_count_u8,
            self.tenant_pk,
            UserCreateArgs {
                user_type: self.user_type,
                cyoa_type: self.cyoa_type,
                client_ip: self.client_ip,
                tunnel_endpoint: self.tunnel_endpoint,
                dz_prefix_count: dz_prefix_count_u8,
                ip_proof: self.ip_proof,
            },
        );

        let signature = match &self.ip_proof {
            Some(proof) => client.send_instructions(instructions_with_ip_proof(
                client,
                proof,
                &client.get_payer(),
                &self.client_ip,
                self.user_type as u8,
                ix,
            )?)?,
            None => client.send_transaction(ix)?,
        };

        Ok((signature, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::create::CreateUserCommand,
        tests::utils::{create_test_client, create_test_client_with_ip_verifier},
        DoubleZeroClient,
    };
    use doublezero_ip_proof::IpOwnershipProof;
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        processors::user::create::UserCreateArgs,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            user::{UserCYOA, UserType},
        },
    };
    use doublezero_serviceability_instruction::{
        ip_proof::with_ed25519_verification, user::create_user,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_user_create() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        // GetAccessPassCommand checks the UNSPECIFIED (dynamic) PDA first; no pass
        // exists there, so it falls back to the exact-IP PDA above.
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        let expected = create_user(
            &program_id,
            &payer,
            &device_pk,
            &accesspass_pubkey,
            1,
            None,
            UserCreateArgs {
                user_type: UserType::IBRLWithAllocatedIP,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip,
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                dz_prefix_count: 1,
                ip_proof: None,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateUserCommand {
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tenant_pk: None,
            ip_proof: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    /// The RFC-27 path. The proof rides in the args (which is what makes the builder append the
    /// Instructions sysvar) and the transaction gains the Ed25519 instruction the program looks
    /// for, with the verifier key read from GlobalState rather than supplied by the caller.
    #[test]
    fn test_commands_user_create_with_ip_proof() {
        let verifier = Pubkey::new_unique();
        let mut client = create_test_client_with_ip_verifier(verifier);

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        seed_accesspass_and_device(&mut client, program_id, payer, device_pk, client_ip);

        let proof = proof_for(payer, client_ip);
        let expected_create = create_user(
            &program_id,
            &payer,
            &device_pk,
            &get_accesspass_pda(&program_id, &client_ip, &payer).0,
            1,
            None,
            UserCreateArgs {
                user_type: UserType::IBRLWithAllocatedIP,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip,
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                dz_prefix_count: 1,
                ip_proof: Some(proof),
            },
        );
        let expected =
            with_ed25519_verification(&verifier, &proof, expected_create.clone()).to_vec();
        client
            .expect_send_instructions()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = command(client_ip, device_pk, Some(proof)).execute(&client);
        assert!(res.is_ok(), "{res:?}");
    }

    /// An unconfigured verifier key is a local failure, not a paid-for transaction that the
    /// program rejects with `IpVerifierNotConfigured`.
    #[test]
    fn test_commands_user_create_with_ip_proof_rejects_unset_verifier() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        seed_accesspass_and_device(&mut client, program_id, payer, device_pk, client_ip);

        let err = command(client_ip, device_pk, Some(proof_for(payer, client_ip)))
            .execute(&client)
            .expect_err("an unset verifier key must not produce a transaction");
        assert!(
            err.to_string().contains("No IP verifier authority"),
            "{err}"
        );
    }

    /// A proof issued for someone else can never validate, so it must not reach the chain.
    #[test]
    fn test_commands_user_create_with_ip_proof_rejects_foreign_payer() {
        let mut client = create_test_client_with_ip_verifier(Pubkey::new_unique());

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        seed_accesspass_and_device(&mut client, program_id, payer, device_pk, client_ip);

        let foreign = proof_for(Pubkey::new_unique(), client_ip);
        let err = command(client_ip, device_pk, Some(foreign))
            .execute(&client)
            .expect_err("a proof naming another payer must not produce a transaction");
        assert!(err.to_string().contains("was issued for"), "{err}");
    }

    /// A proof for a different address pins a different User PDA than the one being created, so
    /// the program would reject it.
    #[test]
    fn test_commands_user_create_with_ip_proof_rejects_mismatched_client_ip() {
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);
        assert_proof_rejected(client_ip, |proof| {
            proof.client_ip = Ipv4Addr::new(198, 51, 100, 4)
        });
    }

    /// Same for the connection type: the User PDA is `f(client_ip, user_type)`, so a proof issued
    /// for one type does not authorize another on the same address.
    #[test]
    fn test_commands_user_create_with_ip_proof_rejects_mismatched_user_type() {
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);
        assert_proof_rejected(client_ip, |proof| {
            proof.user_type = UserType::Multicast as u8
        });
    }

    /// Builds an otherwise valid request whose proof has been bent by `mutate`, and asserts the
    /// command refuses it before any transaction is sent.
    fn assert_proof_rejected(client_ip: Ipv4Addr, mutate: impl FnOnce(&mut IpOwnershipProof)) {
        let mut client = create_test_client_with_ip_verifier(Pubkey::new_unique());

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let device_pk = Pubkey::new_unique();

        seed_accesspass_and_device(&mut client, program_id, payer, device_pk, client_ip);

        let mut proof = proof_for(payer, client_ip);
        mutate(&mut proof);

        let err = command(client_ip, device_pk, Some(proof))
            .execute(&client)
            .expect_err("a mismatched proof must not produce a transaction");
        assert!(err.to_string().contains("was issued for"), "{err}");
    }

    fn command(
        client_ip: Ipv4Addr,
        device_pk: Pubkey,
        ip_proof: Option<IpOwnershipProof>,
    ) -> CreateUserCommand {
        CreateUserCommand {
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tenant_pk: None,
            ip_proof,
        }
    }

    /// The signature is never checked client-side, so its contents are irrelevant here; what
    /// matters is which key the proof names and that the bytes reach the Ed25519 instruction.
    fn proof_for(payer: Pubkey, client_ip: Ipv4Addr) -> IpOwnershipProof {
        IpOwnershipProof {
            version: 1,
            payer,
            client_ip,
            epoch: 931,
            user_type: UserType::IBRLWithAllocatedIP as u8,
            signature: [5u8; 64],
        }
    }

    /// The reads every path through `execute` performs: the exact-IP access pass (after the
    /// dynamic PDA misses) and the device, for its `dz_prefixes`.
    fn seed_accesspass_and_device(
        client: &mut crate::MockDoubleZeroClient,
        program_id: Pubkey,
        payer: Pubkey,
        device_pk: Pubkey,
        client_ip: Ipv4Addr,
    ) {
        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));
    }
}
