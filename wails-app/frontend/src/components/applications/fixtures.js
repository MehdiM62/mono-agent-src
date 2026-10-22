export const mockApplicationDetail = {
  id: 'app-test-1',
  kind: 'job',
  title: 'Lead Systems Architect',
  company: 'OmniCorp',
  url: 'https://careers.omnicorp.example/jobs/992',
  submission_deadline: '2026-11-15',
  status: 'awaiting_submit',
  description: 'Design and deploy multi-agent coordination runtimes.',
  status_log: [
    { created_at: '2026-10-22 09:30:00', from_status: '', to_status: 'pending', actor: 'user' },
    { created_at: '2026-10-22 11:45:00', from_status: 'pending', to_status: 'awaiting_submit', actor: 'agent' },
  ],
  tags: ['go', 'react', 'agents'],
  updated_at: '2026-10-22 11:45:00',
}

export const mockApplicationAssets = {
  posting: {
    url: 'https://careers.omnicorp.example/jobs/992',
    capturedText: 'We are seeking a Lead Systems Architect to spearhead autonomous workflow automation...',
    capturedAt: '2026-10-22 09:31:00',
    deadline: '2026-11-15',
  },
  documents: [
    {
      id: 'doc-cv-1',
      filename: 'CV_Systems_Architect.pdf',
      kind: 'cv',
      source: 'generated',
      format: 'pdf',
      sizeBytes: 24500,
      createdAt: '2026-10-22 10:15:00',
      attached: true,
      path: '/home/monoes/.monoagent/profiles/work/documents/CV_Systems_Architect.pdf',
    },
    {
      id: 'doc-cl-1',
      filename: 'Cover_Letter_OmniCorp.html',
      kind: 'cover_letter',
      source: 'generated',
      format: 'html',
      sizeBytes: 5200,
      createdAt: '2026-10-22 10:16:00',
      attached: true,
      path: '/home/monoes/.monoagent/profiles/work/documents/Cover_Letter_OmniCorp.html',
    },
    {
      id: 'doc-portfolio-1',
      filename: 'Architecture_Portfolio.pdf',
      kind: 'other',
      source: 'upload',
      format: 'pdf',
      sizeBytes: 1048576,
      createdAt: '2026-10-18 14:00:00',
      attached: false,
      path: '/home/monoes/.monoagent/profiles/work/documents/Architecture_Portfolio.pdf',
    },
  ],
  fill: {
    status: 'filled',
    screenshot: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
    updatedAt: '2026-10-22 11:42:00',
    fields: [
      { label: 'Full Legal Name', value: 'Alex Morgan', source: 'profile:name', generated: false },
      { label: 'Email Address', value: 'alex.morgan@example.com', source: 'profile:email', generated: false },
      { label: 'Portfolio URL', value: 'https://alexmorgan.dev', source: 'profile:url', generated: false },
      { label: 'Why do you want to join OmniCorp?', value: 'I have spent years optimizing orchestrators and agent communications.', source: 'agent:llm_synthesis', generated: true },
    ],
    note: 'Form filled up to submit button. Parked awaiting human review and submit click.',
  },
  people: [
    {
      id: 'person-sarah-1',
      name: 'Sarah Connor',
      role: 'Staff Talent Partner',
      email: 'sconnor@omnicorp.example',
    },
  ],
  messages: [
    {
      id: 'msg-thread-101',
      channel: 'email',
      direction: 'in',
      subject: 'Interview inquiry - Lead Systems Architect',
      snippet: 'Hi Alex, we were impressed by your background and would like to confirm your application details...',
      at: '2026-10-22 12:00:00',
    },
  ],
  tasks: [
    {
      id: 'task-board-1',
      title: 'Review OmniCorp submission form in parked browser tab',
      status: 'ready',
    },
  ],
  task: {
    taskId: 'aitask-app-apply-992',
    conversationId: 'conv-coder-8812',
    status: 'needs_user',
  },
  links: [
    { kind: 'person', refId: 'person-sarah-1' },
    { kind: 'message', refId: 'msg-thread-101' },
    { kind: 'task', refId: 'task-board-1' },
  ],
}

export const mockEmptyAssets = {
  posting: null,
  documents: [],
  fill: null,
  people: [],
  messages: [],
  tasks: [],
  task: null,
  links: [],
}
