use indexmap::IndexSet;

#[derive(Debug)]
pub struct IDAllocator {
    pub first: u16,
    pub assigned: IndexSet<u16>,
}

impl IDAllocator {
    pub fn new(first: u16, assigned: Vec<u16>) -> Self {
        Self {
            first,
            assigned: assigned.into_iter().collect(),
        }
    }

    pub fn assign(&mut self, id: u16) {
        self.assigned.insert(id);
    }

    pub fn unassign(&mut self, id: u16) {
        self.assigned.shift_remove(&id);
    }

    pub fn next_available(&mut self) -> u16 {
        let mut id = self.first;
        while self.assigned.contains(&id) {
            id += 1;
        }
        self.assigned.insert(id);
        id
    }

    #[allow(dead_code)]
    pub fn display_assigned(&self) -> String {
        self.assigned
            .iter()
            .map(|id| id.to_string())
            .collect::<Vec<String>>()
            .join(",")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_allocator() {
        let allocator = IDAllocator::new(100, vec![101, 103, 105]);
        assert_eq!(allocator.first, 100);
        assert_eq!(allocator.display_assigned(), "101,103,105");
    }

    #[test]
    fn test_new_with_duplicates() {
        let allocator = IDAllocator::new(100, vec![101, 103, 101, 105, 103]);
        assert_eq!(allocator.display_assigned(), "101,103,105");
    }

    #[test]
    fn test_assign() {
        let mut allocator = IDAllocator::new(100, vec![101, 103]);
        allocator.assign(102);
        assert_eq!(allocator.display_assigned(), "101,103,102");

        // Assign same id => Doesn't show up
        allocator.assign(102);
        assert_eq!(allocator.display_assigned(), "101,103,102");
    }

    #[test]
    fn test_unassign() {
        let mut allocator = IDAllocator::new(100, vec![101, 102, 103]);
        allocator.unassign(102);
        assert_eq!(allocator.display_assigned(), "101,103");

        // Should not be able to assign non-existent ID
        allocator.unassign(999);
        assert_eq!(allocator.display_assigned(), "101,103");
    }

    #[test]
    fn test_next_available_from_first() {
        let mut allocator = IDAllocator::new(100, vec![101, 102]);
        let id = allocator.next_available();
        assert_eq!(id, 100);
        assert_eq!(allocator.display_assigned(), "101,102,100");
    }

    #[test]
    fn test_next_available_fills_gap() {
        let mut allocator = IDAllocator::new(100, vec![100, 101, 103]);
        let id = allocator.next_available();
        assert_eq!(id, 102);
        assert_eq!(allocator.display_assigned(), "100,101,103,102");
    }

    #[test]
    fn test_next_available_after_all_taken() {
        let mut allocator = IDAllocator::new(100, vec![100, 101, 102]);
        let id = allocator.next_available();
        assert_eq!(id, 103);
        assert_eq!(allocator.display_assigned(), "100,101,102,103");
    }

    #[test]
    fn test_reuse_unassigned_id() {
        let mut allocator = IDAllocator::new(100, vec![100, 101, 102]);
        allocator.unassign(101);
        let id = allocator.next_available();
        assert_eq!(id, 101);
        assert_eq!(allocator.display_assigned(), "100,102,101");
    }

    #[test]
    fn test_empty_allocator() {
        let mut allocator = IDAllocator::new(200, vec![]);
        assert_eq!(allocator.display_assigned(), "");

        let id = allocator.next_available();
        assert_eq!(id, 200);
        assert_eq!(allocator.display_assigned(), "200");
    }

    #[test]
    fn test_insertion_order_preserved() {
        let mut allocator = IDAllocator::new(1, vec![5, 3, 7, 2]);
        assert_eq!(allocator.display_assigned(), "5,3,7,2");

        allocator.assign(4);
        allocator.assign(6);
        assert_eq!(allocator.display_assigned(), "5,3,7,2,4,6");
    }
}
