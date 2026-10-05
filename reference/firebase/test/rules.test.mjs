// rules.test.mjs — proves the Firebase rules enforce the same cases as the Postgres RLS demo (M3).
// Run (needs Node 22+ and Java 21+):   cd reference/firebase && npm install && npm test
import { test, before, after } from 'node:test';
import { readFileSync } from 'node:fs';
import {
  initializeTestEnvironment, assertSucceeds, assertFails,
} from '@firebase/rules-unit-testing';

const ACME = 'acme', BUMI = 'bumi';
// Custom claims that our backend would set with the Admin SDK (setCustomUserClaims)
const ana  = { uid: 'ana',  claims: { company_id: ACME, role: 'agent' } };
const budi = { uid: 'budi', claims: { company_id: ACME, role: 'agent' } };
const sari = { uid: 'sari', claims: { company_id: ACME, role: 'supervisor' } };
const dewi = { uid: 'dewi', claims: { company_id: BUMI, role: 'agent' } };

let env;
const as = (u) => env.authenticatedContext(u.uid, u.claims);

before(async () => {
  env = await initializeTestEnvironment({
    projectId: 'demo-supportdesk',            // "demo-" = never touches a real project
    firestore: { rules: readFileSync(new URL('../firestore.rules', import.meta.url), 'utf8') },
    database:  { rules: readFileSync(new URL('../database.rules.json', import.meta.url), 'utf8') },
  });
  // Seed data with rules disabled (= what the Admin SDK can do)
  await env.withSecurityRulesDisabled(async (ctx) => {
    const fs = ctx.firestore(), db = ctx.database();
    const rows = {
      c1: { customer_name: 'Rina',  channel: 'whatsapp', status: 'open', assigned_to: 'ana' },
      c2: { customer_name: 'Tono',  channel: 'email',    status: 'open', assigned_to: 'budi' },
      c3: { customer_name: 'Lia',   channel: 'webchat',  status: 'open', assigned_to: null },
    };
    for (const [id, row] of Object.entries(rows)) {
      await fs.doc(`companies/${ACME}/conversations/${id}`).set(row);
      const { assigned_to, ...rest } = row;  // RTDB has no nulls: "unassigned" = field absent
      await db.ref(`companies/${ACME}/conversations/${id}`).set(assigned_to ? row : rest);
    }
    await fs.doc(`companies/${BUMI}/conversations/b1`).set({ customer_name: 'Hendra', channel: 'email', status: 'open', assigned_to: 'dewi' });
  });
});
after(async () => { await env?.cleanup(); });

// ---------------- Firestore ----------------
const conv = (u, company, id) => as(u).firestore().doc(`companies/${company}/conversations/${id}`);
const convs = (u, company) => as(u).firestore().collection(`companies/${company}/conversations`);

test('Firestore: agent reads own + unassigned, not a colleague\'s', async () => {
  await assertSucceeds(conv(ana, ACME, 'c1').get());
  await assertSucceeds(conv(ana, ACME, 'c3').get());
  await assertFails(conv(ana, ACME, 'c2').get());
});

test('Firestore: rules are not filters — unfiltered list fails, filtered list works', async () => {
  await assertFails(convs(ana, ACME).get());
  await assertSucceeds(convs(ana, ACME).where('assigned_to', '==', 'ana').get());
  await assertSucceeds(convs(sari, ACME).get());               // supervisor may list all
});

test('Firestore: tenant isolation — other company is invisible', async () => {
  await assertFails(conv(dewi, ACME, 'c1').get());
  await assertFails(convs(sari, BUMI).get());
});

test('Firestore: cannot create in another company (spoof)', async () => {
  await assertFails(conv(ana, BUMI, 'x').set({ customer_name: 'X', channel: 'email', status: 'open', assigned_to: null }));
  await assertSucceeds(conv(ana, ACME, 'new1').set({ customer_name: 'Y', channel: 'email', status: 'open', assigned_to: null }));
  await assertFails(conv(ana, ACME, 'new2').set({ customer_name: 'Y', channel: 'email', status: 'open', assigned_to: 'budi' }));
  await assertFails(conv(ana, ACME, 'new3').set({ customer_name: 'Y', channel: 'fax', status: 'open', assigned_to: null }));
  await assertFails(conv(ana, ACME, 'new4').set({ customer_name: 'Y', channel: 'email', status: 'open', assigned_to: null, extra: 1 }));
});

test('Firestore: updates — own status yes, colleague no, agent cannot reassign, bad status no', async () => {
  await assertSucceeds(conv(ana, ACME, 'c1').update({ status: 'pending' }));
  await assertFails(conv(budi, ACME, 'c1').update({ status: 'closed' }));
  await assertFails(conv(ana, ACME, 'c1').update({ assigned_to: 'budi' }));
  await assertFails(conv(ana, ACME, 'c1').update({ status: 'deleted' }));
  await assertFails(conv(ana, ACME, 'c1').update({ customer_name: 'Changed' }));
  await assertSucceeds(conv(sari, ACME, 'c2').update({ assigned_to: 'ana' }));
});

test('Firestore: no login → nothing; no client deletes', async () => {
  await assertFails(env.unauthenticatedContext().firestore().doc(`companies/${ACME}/conversations/c1`).get());
  await assertFails(conv(sari, ACME, 'c3').delete());
});

// ---------------- Realtime Database ----------------
const ref = (u, path) => as(u).database().ref(path);
const list = `companies/${ACME}/conversations`;

test('RTDB: agent reads own + unassigned item, not a colleague\'s', async () => {
  await assertSucceeds(ref(ana, `${list}/c1`).get());
  await assertSucceeds(ref(ana, `${list}/c3`).get());
  await assertFails(ref(ana, `${list}/c2`).get());
});

test('RTDB: list needs a matching query (query-based rules), supervisor can list all', async () => {
  await assertFails(ref(ana, list).get());
  await assertSucceeds(ref(ana, list).orderByChild('assigned_to').equalTo('ana').get());
  await assertFails(ref(ana, list).orderByChild('assigned_to').equalTo('budi').get());
  await assertSucceeds(ref(sari, list).get());
});

test('RTDB: tenant isolation + writes', async () => {
  await assertFails(ref(dewi, `${list}/c1`).get());
  await assertSucceeds(ref(ana, `${list}/c1/status`).set('pending'));
  await assertFails(ref(ana, `${list}/c1/status`).set('deleted'));        // .validate
  await assertFails(ref(budi, `${list}/c1/status`).set('closed'));         // not his
  await assertFails(ref(ana, `${list}/c1/assigned_to`).set('budi'));       // agent cannot reassign
  await assertFails(ref(ana, `${list}/c1/assigned_to`).remove());          // ...or un-assign
  await assertFails(ref(ana, `${list}/c1/customer_name`).set('Changed'));  // agent changes only status
  await assertSucceeds(ref(ana, `${list}/new1`).set({ customer_name: 'Y', channel: 'email', status: 'open' }));
  await assertFails(ref(ana, `${list}/new2`).set({ customer_name: 'Y', channel: 'email', status: 'open', assigned_to: 'budi' }));
  await assertFails(ref(ana, `companies/${BUMI}/conversations/z`).set({ customer_name: 'X', channel: 'email', status: 'open' }));
  await assertFails(ref(sari, `${list}/c3`).remove());                     // no deletes
});
