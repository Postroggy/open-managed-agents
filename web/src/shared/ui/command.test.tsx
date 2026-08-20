import { afterEach, describe, expect, test } from 'bun:test';
import '../../test/setup';
import { Command, CommandInput } from './command';

const { cleanup, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

describe('CommandInput', () => {
  test('marks the nested input for the command-specific focus style', () => {
    render(
      <Command>
        <CommandInput aria-label="搜索" />
      </Command>,
    );

    expect(screen.getByRole('combobox').dataset.slot).toBe('command-input');
  });

  test('removes the global focus ring from command and search inputs', async () => {
    const stylesheet = await Bun.file(new URL('../../styles/foundation.css', import.meta.url)).text();

    expect(stylesheet).toContain(":focus-visible:not(\n    :where([data-slot='input'], [data-slot='textarea'])\n  )");
    expect(stylesheet).toContain(
      ":where([data-slot='command-input'], input[type='search']):focus-visible {\n  box-shadow: none;\n}",
    );
  });
});
