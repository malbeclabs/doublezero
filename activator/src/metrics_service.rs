use std::collections::HashMap;

use mockall::automock;

pub type TagMap = HashMap<String, String>;
pub type FieldMap = HashMap<String, f64>;

#[derive(Debug, PartialEq)]
pub struct Metric {
    pub measurement: String,
    pub tags: TagMap,
    pub fields: FieldMap,
}

impl Metric {
    pub fn new(measurement: &str) -> Self {
        Metric {
            measurement: measurement.to_string(),
            tags: TagMap::default(),
            fields: FieldMap::default(),
        }
    }

    pub fn add_tag(&mut self, tag: &str, val: &str) -> &mut Self {
        self.tags.insert(tag.to_string(), val.to_string());
        self
    }

    pub fn add_field(&mut self, field: &str, val: f64) -> &mut Self {
        self.fields.insert(field.to_string(), val);
        self
    }
}

#[automock]
pub trait MetricsService: Send + Sync + 'static {
    #[allow(dead_code)]
    fn write_metric(&self, metric: &Metric);
    fn write_metrics(&self, metrics: &Vec<Metric>);
}
