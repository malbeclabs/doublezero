use crate::fee_payment_calculator::ValidatorRewards;
use anyhow::{anyhow, Result};
use futures::{stream, StreamExt};
use serde::Deserialize;
use std::collections::HashMap;

const JITO_BASE_URL: &str = "https://kobe.mainnet.jito.network/api/v1/";

pub const JITO_REWARDS_LIMIT: u16 = 1_500;

#[derive(Deserialize, Debug)]
pub struct JitoRewards {
    // TODO: check total_count to see if it exceeds entries in a single response
    // limit - default: 100, max: 10000
    pub total_count: u16,
    pub rewards: Vec<JitoReward>,
}

#[derive(Deserialize, Debug)]
pub struct JitoReward {
    pub vote_account: String,
    pub mev_revenue: u64,
}

// may need to add in pagination
pub async fn get_jito_rewards<T: ValidatorRewards>(
    fee_payment_calculator: &T,
    validator_ids: &[String],
    epoch: u64,
) -> Result<HashMap<String, u64>> {
    let url = format!(
        // TODO: make limit an env var
        // based on very unscientific checking of a number of epochs, 1200 is the highest count
        "{JITO_BASE_URL}validator_rewards?epoch={epoch}&limit={JITO_REWARDS_LIMIT}"
    );

    let rewards = match fee_payment_calculator.get::<JitoRewards>(&url).await {
        Ok(jito_rewards) => {
            if jito_rewards.total_count > JITO_REWARDS_LIMIT {
                println!(
                    "Unexpectedly received total count higher than 1500; actual count is {}",
                    jito_rewards.total_count
                );
            }
            jito_rewards
        }

        Err(e) => {
            return Err(anyhow!(
                "Failed to fetch Jito rewards for epoch {epoch}: {e:#?}"
            ));
        }
    };

    let jito_rewards: HashMap<String, u64> = stream::iter(validator_ids)
        .map(|validator_id| {
            let validator_id = validator_id.to_string();
            let rewards = &rewards.rewards;
            async move {
                let mev_revenue = rewards
                    .iter()
                    .find(|reward| *validator_id == reward.vote_account)
                    .map(|reward| reward.mev_revenue)
                    .unwrap_or(0);
                (validator_id, mev_revenue)
            }
        })
        .buffer_unordered(5)
        .collect()
        .await;

    Ok(jito_rewards)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fee_payment_calculator::MockValidatorRewards;

    #[tokio::test]
    async fn test_get_jito_rewards() {
        let mut jito_mock_fetcher = MockValidatorRewards::new();
        let pubkey = "CvSb7wdQAFpHuSpTYTJnX5SYH4hCfQ9VuGnqrKaKwycB";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 812;
        let expected_mev_revenue = 503423196855;
        jito_mock_fetcher
            .expect_get::<JitoRewards>()
            .withf(move |url| url.contains(&format!("epoch={epoch}")))
            .times(1)
            .returning(move |_| {
                Ok(JitoRewards {
                    total_count: 1000,
                    rewards: vec![JitoReward {
                        vote_account: pubkey.to_string(),
                        mev_revenue: expected_mev_revenue,
                    }],
                })
            });

        let mock_response = get_jito_rewards(&jito_mock_fetcher, validator_ids, epoch)
            .await
            .unwrap();

        assert_eq!(mock_response.get(pubkey), Some(&expected_mev_revenue));
    }
}
