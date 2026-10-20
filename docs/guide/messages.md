# Topics and messages

The topic page of a cluster lists the partitions and configs of a topic and has two tabs for
records: **Messages** to read them and **Produce** to write them. Both need a cluster that allows
browsing messages; producing and every change need the operator or admin role.

## Reading records

Choose where to start (the latest records, the earliest, an offset or a time), a partition or all
of them, and how many records to read. The limit is split evenly between the partitions read.
**Older** and **Newer** move to the preceding or the following page in every partition at once.

- Keys, values and header values that are not valid UTF-8 are shown as base64 and marked
  `base64`.
- A record without a value is a tombstone and is marked as such.
- Values longer than 64 KiB are cut in the view.

The API takes the same choices: `GET /api/v1/clusters/{id}/topics/{topic}/messages` with `from`
(`latest`, `earliest`, `offset`, `timestamp`), and the `next` and `previous` cursors of a response
with `from=after` or `from=before` (see the [REST API](../../api/openapi.yaml)).

## Producing records

Choose a partition or leave it to the partitioner, then a key, a value and headers. Each of them is
text or base64 for binary data. The **Tombstone** mode produces a record without a value: in a
compacted topic it removes the earlier records with the same key once compaction runs. Compacted
topics reject records without a key.

## Removing records

Kafka keeps records until retention or compaction removes them. Without Brokerium Enterprise:

- **Delete the topic** removes it with all records; Brokerium asks for the topic name first.
- **Lower `retention.ms`** for a while, then restore it: Kafka removes whole log segments that are
  older than the retention, so the cut is not exact, and only in topics whose `cleanup.policy`
  includes `delete`.
- **Produce tombstones** to remove keys from a compacted topic.

### Delete records and empty topics (Enterprise)

**Delete records** on the topic page removes the records before a point in each partition with
Kafka's DeleteRecords, which moves the start offset of the partition. Choose one of:

- **Before offsets:** an offset per partition; the records before it are deleted.
- **Before a time:** the records older than a time, in every partition.
- **All records:** empties the topic; the topic, its configs and its partitions stay.

**Preview** shows how many records each partition loses and its new start offset; nothing changes
until you confirm with the topic name. The deletion is recorded in the audit log as
`topic.delete-records`. Consumer groups whose committed offsets fall before the new start offsets
reset according to their `auto.offset.reset`. Kafka frees the disk space when it deletes the
affected log segments.

Kafka deletes records only in topics whose `cleanup.policy` includes `delete`; for a compacted
topic the API answers 409 `compacted_topic`. The API is
`POST /api/v1/clusters/{id}/topics/{topic}/delete-records` with `dryRun` and `confirm`.
