# Content team with a publisher automation

An editor and a writer draft a post; a **Publish post** workflow joins the
org as an automation role. When the editor messages it, the workflow
formats the post, pauses for review (a HIL item the org's autonomy settings
route), and sends it to Slack. Its result comes back to the editor as a
reply.

Needs monomind with capabilities `org-tool-providers` and
`org-endpoint-roles`, and a Slack connection
(`monoagentcli --profile work connect slack`).

```bash
# 1. The workflow. Set the Slack channel first (REPLACE_WITH_CHANNEL_ID).
WF=$(monoagentcli --profile work --json workflow import --file examples/orgs/content-team/publish-post.json | jq -r .id)

# 2. The org, in the profile's folder (starts at autonomy mid).
monoagentcli --profile work org create-json content-team --json "$(cat examples/orgs/content-team/content-team.json)"

# 3. The workflow joins the org and becomes a role the editor can message.
monoagentcli --profile work org automation add content-team --workflow "$WF" --alias publish_post --owned
monoagentcli --profile work org automation-role add content-team --alias publish_post --reports-to editor

# 4. Who approves the review step: at mid a person decides irreversible items
#    (this workflow sends to Slack, so its HIL item is irreversible).
monoagentcli --profile work org autonomy show content-team

# 5. Run it. Both daemons must be up.
monoagentcli daemon &
monoagentcli --profile work org serve
monoagentcli --profile work org run content-team --task "Write about offline-first workflows"
monoagentcli --profile work org autonomy needs-you content-team   # approve the review here or in the GUI
```

To let the editor call the workflow as a tool instead of messaging a role,
grant it (and drop step 3's `automation-role add`):

```bash
monoagentcli --profile work org grant add content-team --role editor --automation publish_post
```
