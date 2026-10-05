-- 05-seed.sql — demo data: two companies, five users, nine conversations.
-- Runs as postgres (superuser), which bypasses RLS — normal for seeding/migrations.

INSERT INTO support.companies (id, name) VALUES
  ('11111111-1111-1111-1111-111111111111', 'Acme Retail'),
  ('22222222-2222-2222-2222-222222222222', 'Bumi Coffee');

INSERT INTO support.users (id, company_id, name, email, role) VALUES
  ('a0000000-0000-0000-0000-00000000000a', '11111111-1111-1111-1111-111111111111', 'Ana',  'ana@acme.test',  'agent'),
  ('b0000000-0000-0000-0000-00000000000b', '11111111-1111-1111-1111-111111111111', 'Budi', 'budi@acme.test', 'agent'),
  ('c0000000-0000-0000-0000-00000000000c', '11111111-1111-1111-1111-111111111111', 'Sari', 'sari@acme.test', 'supervisor'),
  ('d0000000-0000-0000-0000-00000000000d', '22222222-2222-2222-2222-222222222222', 'Dewi', 'dewi@bumi.test', 'agent'),
  ('e0000000-0000-0000-0000-00000000000e', '22222222-2222-2222-2222-222222222222', 'Eko',  'eko@bumi.test',  'supervisor');

INSERT INTO support.conversations (company_id, assigned_to, customer_name, channel, subject, status) VALUES
  -- Acme Retail
  ('11111111-1111-1111-1111-111111111111', 'a0000000-0000-0000-0000-00000000000a', 'Rina Wijaya',   'whatsapp',  'Order #1042 not delivered',      'open'),
  ('11111111-1111-1111-1111-111111111111', 'a0000000-0000-0000-0000-00000000000a', 'Tono Hartono',  'instagram', 'Size exchange for jacket',        'pending'),
  ('11111111-1111-1111-1111-111111111111', 'b0000000-0000-0000-0000-00000000000b', 'Lia Santoso',   'whatsapp',  'Refund for damaged item',         'open'),
  ('11111111-1111-1111-1111-111111111111', 'b0000000-0000-0000-0000-00000000000b', 'Agus Pratama',  'email',     'Invoice copy request',            'closed'),
  ('11111111-1111-1111-1111-111111111111', NULL,                                   'Maya Putri',    'webchat',   'Store opening hours?',            'open'),
  -- Bumi Coffee
  ('22222222-2222-2222-2222-222222222222', 'd0000000-0000-0000-0000-00000000000d', 'Hendra Gunawan','whatsapp',  'Wrong beans in subscription box', 'open'),
  ('22222222-2222-2222-2222-222222222222', 'd0000000-0000-0000-0000-00000000000d', 'Sinta Dewanti', 'instagram', 'Collaboration inquiry',           'pending'),
  ('22222222-2222-2222-2222-222222222222', NULL,                                   'Yusuf Rahman',  'whatsapp',  'Wholesale price list',            'open'),
  ('22222222-2222-2222-2222-222222222222', 'e0000000-0000-0000-0000-00000000000e', 'Putri Ayu',     'email',     'Complaint escalated to manager',  'open');
