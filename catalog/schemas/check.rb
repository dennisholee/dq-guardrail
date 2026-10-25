#!/usr/bin/env ruby
# Structural self-check for the schema registry.
#
# Asserts, for every *.schema.yaml in this directory:
#   1. it parses as YAML
#   2. it declares the JSON Schema 2020-12 dialect
#   3. it declares an absolute https $id
#   4. every $ref resolves — internal (#/$defs/x) against itself, external
#      (file.schema.yaml#/$defs/x) against the named sibling file
#
# This is not a full meta-schema validation; it catches the defects that actually
# occur (typos in $defs, dangling cross-file references) without a network install.
require 'yaml'
require 'date'

DIR = __dir__
DOCS = {}

Dir.glob(File.join(DIR, '*.schema.yaml')).sort.each do |path|
  DOCS[File.basename(path)] = YAML.safe_load(File.read(path), aliases: true)
end

def refs_in(node, found = [])
  case node
  when Hash
    node.each do |k, v|
      k == '$ref' && v.is_a?(String) ? found << v : refs_in(v, found)
    end
  when Array
    node.each { |v| refs_in(v, found) }
  end
  found
end

def dig_pointer(doc, fragment)
  parts = fragment.sub(%r{\A/}, '').split('/').map { |p| p.gsub('~1', '/').gsub('~0', '~') }
  parts.reduce(doc) { |node, key| node.is_a?(Hash) ? node[key] : nil }
end

problems = []

DOCS.each do |name, doc|
  unless doc.is_a?(Hash)
    problems << "#{name}: root is not a mapping"
    next
  end
  problems << "#{name}: missing $schema" unless doc['$schema']
  unless doc['$schema'] == 'https://json-schema.org/draft/2020-12/schema'
    problems << "#{name}: $schema is #{doc['$schema'].inspect}, expected 2020-12"
  end
  id = doc['$id']
  problems << "#{name}: missing $id" unless id
  problems << "#{name}: $id must be absolute https, got #{id.inspect}" if id && !id.start_with?('https://')

  refs_in(doc).uniq.each do |ref|
    file_part, fragment = ref.split('#', 2)
    target_name = file_part.to_s.empty? ? name : File.basename(file_part)
    target = DOCS[target_name]
    if target.nil?
      problems << "#{name}: $ref #{ref} points at unknown file #{target_name}"
      next
    end
    next if fragment.nil? || fragment.empty?

    problems << "#{name}: $ref #{ref} does not resolve" if dig_pointer(target, fragment).nil?
  end
end

puts "checked #{DOCS.size} schema(s)"
problems.each { |p| puts "  broken: #{p}" }
puts(problems.empty? ? 'all refs resolve, all dialects declared' : "#{problems.size} problem(s)")
exit(problems.empty? ? 0 : 1)
