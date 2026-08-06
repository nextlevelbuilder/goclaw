# receiptexport

Signs exported spans as Ed25519 receipts so a third party can verify what a run
did without trusting the collector, the database, or the operator.

Implements `tracing.SpanExporter`, so it attaches the same way OTLP does and
changes nothing in the collector.

## Why this is not just a trace

A trace says what the runtime emitted. A receipt says what a named key attested
to, in what order, and whether that record has been altered since. The
difference matters the moment a run crosses an organisational boundary, or
someone has to explain a quiet failure after the fact.

## Usage

```go
key, err := receiptexport.GenerateSigningKey()
if err != nil {
    return err
}

sink, err := receiptexport.NewFileSink(
    "/var/lib/goclaw/receipts.jsonl",
    "/var/lib/goclaw/checkpoint.json",
)
if err != nil {
    return err
}

exporter, err := receiptexport.New(receiptexport.Config{
    Key:             key,
    Sink:            sink,
    SpanTypes:       []string{"tool_call"}, // omit to sign everything
    CheckpointEvery: 100,
})
if err != nil {
    return err
}

collector.SetExporter(exporter)
```

Publish only the public half for verifiers:

```go
keyring := receiptexport.NewKeyring(key.PublicEntry())
```

Verify with nothing but the evidence and that keyring:

```go
receipts, checkpoint, err := receiptexport.LoadBundle(receiptPath, checkpointPath)
report, err := receiptexport.VerifyReceipts(receipts, checkpoint, keyring)
```

## The checkpoint is not optional

A hash chain detects modification, reordering, and deletion from the middle. It
cannot detect truncation: remove the newest receipts and every remaining link
still verifies clean.

The checkpoint closes that by committing to a receipt count and a terminal
hash. `VerifyReceipts` reports `Valid: false` when no checkpoint is supplied,
because without one it can only say "nothing I was given has been altered",
never "nothing is missing".

**A checkpoint stored beside the receipts it anchors is deletable by whoever
deletes the receipts.** To be worth anything it has to reach somewhere the
operator cannot quietly rewrite: a build log, an object store with retention, or
the commit the run produced. `Sink` keeps receipt and checkpoint destinations
separate so that publishing step is easy to add.

## Coverage, and what it does not claim

`Collector.EmitSpan` drops spans when its buffer is full. That is correct for
observability, since tracing must never block a run, and it means an exporter
can only sign what it receives.

So `Coverage` counts spans **this exporter saw**, never spans the runtime
executed, and it carries a note saying so into every checkpoint. A signed
receipt set is a floor on what happened, not a complete record.

Quantifying the gap would need a drop counter in the collector, which this
package deliberately does not add. Happy to follow up with one if it is wanted.

## What a verified receipt establishes

> This span, with exactly these fields, was exported by the holder of this key
> at this time, and the verified sequence has not been altered since.

## What it does not establish

Returned in every verification report rather than left to a reader to infer:

- Not that these are all the spans the runtime produced.
- Not that a tool call was correct, authorised, or safe.
- Not that receipts signed by a compromised key were truthful.
- Without a checkpoint, not that receipts were not removed from the end.

## Design notes

**Content is committed by digest, not copied.** The tracing package redacts
previews for a reason. Duplicating that content into a second, separately
stored artifact would widen the blast radius of a leak while adding nothing a
digest does not already prove.

**Ordering comes from an exporter sequence, not a clock.** Ordering evidence by
a timestamp invites an older stamp recorded later to make honest history look
altered.

**Revocation is not retroactive.** A receipt signed while the key was trusted
stays valid, because the alternative silently voids honest history every time
an operator rotates. `RevokedAt` exists for the compromise case.

**Signature and chain failures are reported separately.** They are different
failures calling for different responses, and a deleted receipt leaves every
remaining signature genuine.

**A failed write does not advance the chain head**, so a transient sink outage
cannot produce a permanent break that looks identical to tampering.

**Standard library only**, plus `google/uuid` which the repository already uses.
