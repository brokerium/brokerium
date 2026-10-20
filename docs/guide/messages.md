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

## Searching and exporting records (Enterprise)

The **Search** tab of the topic page finds records by key, value or header (a text they contain or
a regular expression in RE2 syntax, optionally ignoring case), a time range and a partition. A
search reads from the start of the time range, or from the earliest record, and stops after the
chosen number of matches, a million records read or 30 seconds; **Continue** reads on from where
it stopped. Only committed records of transactions are read.

**Export** writes every record that matches, up to a million, to a file: NDJSON with one record
per line, a JSON array or CSV. Values are complete; keys, values and header values that are not
UTF-8 are base64 and marked so, and CSV marks null keys and values with the encoding `null`.
Exports are recorded in the audit log as `topic.export`.

The API is `POST /api/v1/clusters/{id}/topics/{topic}/search` and `.../export`.

## Copying and replaying records (Enterprise)

**Copy messages** on the topic page copies the records that exist now and match a filter to a
topic of the same or another cluster; copying into the same topic replays them. The target topic
must exist. Brokerium plans a `copy-messages` operation with one step per partition, and the copy
starts once you confirm it on the operation page, where it can be paused or cancelled like any
operation.

- **Partitioning:** the same partition number (the default when the target has enough
  partitions) or the partitioner of the target, which places records by key.
- **Throttle:** 10 MiB/s by default.
- **Timestamps** are kept unless you turn that off; a target topic with
  `message.timestamp.type=LogAppendTime` sets its own.
- Copies are at least once: a restart of the server during a step copies the records since its
  last recorded progress again, so the target can receive up to a second of records twice.

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
