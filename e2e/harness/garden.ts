// The seeded people and plants the tests name, copied from the Go seed. An id
// here is the one the seed writes, so a test reaches the row the server draws
// without asking the database.

export const people = {
  ellie: { name: 'Ellie', handle: 'ellie' },
  sam: { name: 'Sam', handle: 'sam' },
} as const;

export const plants = {
  bigFella: { id: '00000000-0000-7000-8000-050000000001', name: 'Big Fella' },
  gerald: { id: '00000000-0000-7000-8000-050000000002', name: 'Gerald' },
  doris: { id: '00000000-0000-7000-8000-050000000003', name: 'Doris' },
  nigel: { id: '00000000-0000-7000-8000-050000000005', name: 'Nigel' },
  trailMix: { id: '00000000-0000-7000-8000-050000000007', name: 'Trail Mix' },
  motherInLaw: { id: '00000000-0000-7000-8000-050000000004', name: 'Mother-in-Law' },
} as const;

export type Plant = (typeof plants)[keyof typeof plants];
