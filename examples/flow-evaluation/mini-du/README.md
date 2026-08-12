# mini-du flow evaluation corpus

`validator/` is the portable post-run oracle for the `mini-du` flow cases. It
reads one `takt-evaluation-validator/v1alpha1` request from stdin and returns
one `takt-validation/v1alpha1` result. Product failures are measured as
`valid:false`; malformed requests or an unavailable host `du` oracle exit 2.

The candidate contract is `mini-du [-s] [-k] PATH...`. The validator builds the
candidate, rejects delegation to host `du`, compares fixed filesystem scenarios
against host `du -k`, and checks the declared path/artifact/SCM requirements.
