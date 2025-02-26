#[derive(Debug)]
pub struct IDAllocator {
    pub first: u16,
    pub assigned: Vec<u16>,
}

impl IDAllocator {
    pub fn new(first: u16, assigned: Vec<u16>) -> Self {
        Self {
            first,
            assigned,
        }
    }

    pub fn assign(&mut self, id: u16) {
        self.assigned.push(id);
    }

    pub fn unassign(&mut self, id: u16) {
        self.assigned.retain(|&x| x != id);
    }

    pub fn next_available(&mut self) -> u16 {
        let mut id = self.first;
        while self.assigned.contains(&id) {
            id += 1;
        }
        self.assigned.push(id);
        id
    }
}