# Local-First Encrypted Sync System (LFESS)
## Product Requirements Document (Final Vision)

---

## 1. Overview

LFESS is a local-first, end-to-end encrypted, distributed data system that enables multiple devices (and eventually multiple users) to edit shared data offline and synchronize without conflicts or data loss.

The system prioritizes:
- Data ownership
- Offline-first functionality
- Deterministic conflict resolution
- Zero-trust architecture

---

## 2. Goals

### Primary Goals
- Ensure no data loss under any condition
- Enable offline-first data creation and editing
- Achieve deterministic convergence across devices
- Maintain end-to-end encryption at all times

### Secondary Goals
- Enable peer-to-peer sync without central dependency
- Support multi-device and multi-user scenarios
- Provide a clean, extensible engine architecture

---

## 3. Non-Goals

- Building a full-featured productivity app (e.g. Notion clone)
- Complex UI/UX in early stages
- Real-time collaboration (initially)
- Cloud-first architecture

---

## 4. Core Principles

1. Local-first: All data originates and persists locally
2. Append-only: No destructive updates
3. Deterministic merge: Same inputs → same outputs
4. Encryption-first: All data is encrypted at rest and in transit
5. Device autonomy: Each device operates independently

---

## 5. System Components

### 5.1 Core Engine
- Operation log manager
- Merge engine
- Encryption layer
- Sync protocol handler

### 5.2 Storage Layer
- Local persistent storage
- Append-only operation log
- Snapshot reconstruction

### 5.3 Sync Layer
- Device-to-device communication
- Operation exchange protocol
- Sync state tracking

### 5.4 Identity Layer
- Device-based identity (public/private keys)
- Trust establishment between devices

### 5.5 Application Layer
- Minimal interface (notes / todo)
- Uses engine APIs only

---

## 6. Functional Requirements

### 6.1 Data Operations
- Create item
- Update item
- Delete item
- Each operation must:
  - Have unique ID
  - Be timestamped/logical ordered
  - Be immutable

---

### 6.2 Operation Log
- Append-only structure
- Stores all changes as events
- Supports replay to reconstruct state

---

### 6.3 Merge Engine
- Combines operation logs from multiple devices
- Guarantees:
  - No data loss
  - Eventual consistency
- Handles:
  - Concurrent updates
  - Deletion conflicts

---

### 6.4 Encryption
- All operations encrypted before storage
- Secure key generation per device
- Data unreadable without keys

---

### 6.5 Sync
- Exchange only missing operations
- Support:
  - Manual sync
  - Local network sync
- Maintain sync state per peer

---

### 6.6 Device Linking
- Pair devices using:
  - QR code or shared secret
- Establish trust via key exchange

---

### 6.7 Multi-User (Advanced)
- Shared data across identities
- Permission management
- Access revocation

---

## 7. Non-Functional Requirements

### Reliability
- Zero silent data loss
- Recovery from partial sync failures

### Performance
- Fast local writes
- Efficient merge operations

### Security
- End-to-end encryption
- No plaintext storage

### Consistency
- Eventual consistency across devices
- Deterministic state resolution

---

## 8. Invariants

The system must ALWAYS satisfy:

1. No operation is lost once created
2. Same set of operations → same final state
3. Order of receiving operations does not break consistency
4. Data remains decryptable only with valid keys
5. Merge never deletes valid data unintentionally

---

## 9. Failure Scenarios

- Device goes offline → continues functioning
- Partial sync → resumes without corruption
- Conflicting edits → resolved deterministically
- Device crash → state recoverable from log

---

## 10. Success Criteria

- Multiple devices converge to identical state
- No manual conflict resolution required
- Data remains secure and private
- System functions fully offline

---

## 11. Demo Expectations

- Offline edits across devices → later sync
- Conflict scenario handled automatically
- Encrypted data storage demonstration
- Device pairing and sync flow

---