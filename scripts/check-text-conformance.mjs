// Offline companion to Go tests. Uses native ECMAScript RegExp, not a rewritten
// pattern. Checks the selected string subschemas, not complete JSON documents.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const root = new URL('../contracts/', import.meta.url);
const load = (path) => JSON.parse(readFileSync(new URL(path, root), 'utf8'));
const cases = load('conformance/v0alpha1/text/whitespace.json');
const schemas = new Map();
function document(name) {
  if (!schemas.has(name)) schemas.set(name, load(`schemas/v0alpha1/${name}`));
  return schemas.get(name);
}
function resolve(name, pointer) {
  return pointer.split('/').slice(1).reduce((value, key) => value[key], document(name));
}
function valid(schema, value, name) {
  const supported = new Set(['type', 'minLength', 'maxLength', 'pattern', '$ref', 'allOf']);
  for (const key of Object.keys(schema)) assert(supported.has(key), `unsupported string keyword: ${key}`);
  if (schema.$ref) {
    const [file, pointer] = schema.$ref.split('#');
    if (!valid(resolve(file || name, pointer), value, file || name)) return false;
  }
  if (schema.allOf && !schema.allOf.every((part) => valid(part, value, name))) return false;
  if (schema.type && schema.type !== 'string') throw new Error('not a string subschema');
  const length = [...value].length;
  return (schema.minLength === undefined || length >= schema.minLength)
    && (schema.maxLength === undefined || length <= schema.maxLength)
    && (schema.pattern === undefined || new RegExp(schema.pattern, 'u').test(value));
}

const fields = [
  ['execution-trace', '/$defs/text', 'ecmaNonBlank', 256],
  ['execution-trace', '/$defs/attempt/properties/provider', 'ecmaNonBlank', 128],
  ['decision', '/$defs/evaluator/properties/adapterVersion', 'ecmaNonBlank', 128],
  ['decision', '/$defs/evaluator/properties/fixtureVersion', 'ecmaNonBlank', 128],
  ['evaluator-profile', '/$defs/nonBlankVersion', 'unicodeNonBlank', 128],
  ['evaluator-profile', '/properties/metadata/properties/description', 'unicodeNonBlank', 4096],
  ['regression-suite', '/$defs/versionedIdentity/properties/version', 'unicodeNonBlank', 128],
];
for (const [base, pointer, expectation, limit] of fields) {
  const name = `${base}.schema.json`;
  const schema = resolve(name, pointer);
  for (const test of cases) {
    assert.equal(valid(schema, test.value, name), test[expectation], `${base}${pointer}: ${test.name}`);
  }
  assert(valid(schema, '😀'.repeat(limit), name), `${base}${pointer}: code-point limit`);
  assert(!valid(schema, '😀'.repeat(limit + 1), name), `${base}${pointer}: over limit`);
}
console.log(`PASS: ${cases.length} shared whitespace cases and code-point bounds across ${fields.length} published string subschemas`);
