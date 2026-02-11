import { describe, expect, it } from 'vitest';

import { shapeData } from '../src/core/output-shape.js';

describe('output shape', () => {
  it('applies field projection to object list', () => {
    const shaped = shapeData({
      data: [
        { a: 1, b: 2, c: 3 },
        { a: 4, b: 5, c: 6 }
      ],
      fields: ['a', 'c']
    });

    expect(shaped.data).toEqual([
      { a: 1, c: 3 },
      { a: 4, c: 6 }
    ]);
  });

  it('applies output limit with metadata', () => {
    const shaped = shapeData({
      data: [{ x: 1 }, { x: 2 }, { x: 3 }],
      limit: 2
    });

    expect(shaped.data).toEqual([{ x: 1 }, { x: 2 }]);
    expect(shaped.metaUpdates.total_count).toBe(3);
    expect(shaped.metaUpdates.returned_count).toBe(2);
    expect(shaped.metaUpdates.truncated).toBe(true);
  });
});
