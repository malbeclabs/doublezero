#[cfg(test)]
use metrics::Label;

#[cfg(test)]
use metrics_util::{
    debugging::{DebugValue, Snapshot},
    CompositeKey,
};

#[cfg(test)]
#[derive(Clone, Debug, PartialEq, PartialOrd, Eq, Ord)]
struct Counter {
    name: String,
    labels: Vec<Label>,
    value: u64,
}

#[cfg(test)]
pub struct MetricsSnapshot {
    snapshot: Vec<(CompositeKey, DebugValue)>,
    counter_expectations: Vec<Counter>,
}

#[cfg(test)]
impl MetricsSnapshot {
    pub fn new(snapshot: Snapshot) -> Self {
        Self {
            snapshot: snapshot
                .into_vec()
                .into_iter()
                .map(|(k, _, _, v)| (k, v))
                .collect(),
            counter_expectations: vec![],
        }
    }

    pub fn expect_counter<T1: ToString, T2: ToString, T3: ToString>(
        &mut self,
        name: T1,
        labels: Vec<(T2, T3)>,
        value: u64,
    ) -> &mut Self {
        self.counter_expectations.push(Counter {
            name: name.to_string(),
            labels: labels
                .into_iter()
                .map(|(k, v)| Label::new(k.to_string(), v.to_string()))
                .collect(),
            value,
        });
        self
    }

    pub fn verify(&mut self) {
        let mut counters: Vec<Counter> = vec![];

        for (key, value) in self.snapshot.iter() {
            if let metrics_util::debugging::DebugValue::Counter(val) = value {
                counters.push(Counter {
                    name: key.key().name().to_string(),
                    labels: key.key().labels().cloned().collect(),
                    value: *val,
                });
            }
        }

        counters.sort();
        self.counter_expectations.sort();

        assert_eq!(counters, self.counter_expectations);
    }
}
