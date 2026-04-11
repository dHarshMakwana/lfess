# LFESS - MVP Specification

---

## 1. Objective

Build the smallest possible system that proves:

- Local-first data handling
- Sync between two devices
- Merge without data loss

---

## 2. Scope

### Included
- Single user
- Multiple devices (simulated)
- Basic list data model
- Manual or local sync
- Basic encryption

### Excluded
- Multi-user support
- Real-time sync
- Advanced UI
- Complex conflict resolution

---

## 3. Data Model

### Entity: Item
- id (unique)
- content (string)
- deleted (boolean)

---

### Operation Types
- ADD
- UPDATE
- DELETE

---

### Operation Structure
- operation_id
- item_id
- type
- payload
- timestamp (or logical clock)

---

## 4. Core Features

### 4.1 Local Storage
- Store operation log locally
- Rebuild state by replaying log

---

### 4.2 Operation Handling
- Every change creates an operation
- No direct state mutation

---

### 4.3 Merge Logic
- Combine operation logs
- Remove duplicates
- Apply operations deterministically

---

### 4.4 Sync (Basic)
- Export operations
- Import operations
- Apply missing operations

---

### 4.5 Encryption (Minimal)
- Encrypt operations before storing
- Decrypt before applying

---

## 5. Sync Flow

1. Device A exports operations
2. Device B imports operations
3. Device B merges logs
4. Both devices converge

---

## 6. Success Scenarios

### Scenario 1
- Device A adds item
- Device B adds item
- Sync → both see both items

---

### Scenario 2
- Device A deletes item
- Device B updates same item
- Merge rule applied consistently

---

## 7. Constraints

- No central server
- No real-time communication required
- No UI complexity

---

## 8. Deliverables

- Working CLI or minimal interface
- Operation log system
- Merge working across two instances
- Demonstration of sync flow

---

## 9. Completion Criteria

MVP is complete when:

- Two devices can operate independently
- Data merges without loss
- System works fully offline
- Sync works via manual or local method

---
