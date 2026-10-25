# Architecture diagrams as code.
#
#   make figures    render puml/ -> figures/ (SVG)
#   make validate   XML-validate every SVG (rendered and hand-authored)
#   make links      resolve every internal and cross-document markdown link
#   make check      validate + links
#   make clean      remove rendered SVGs
#
# PlantUML is pinned by version so renders are reproducible across machines.
# Override with: make figures PLANTUML_JAR=/path/to/plantuml.jar

PLANTUML_VERSION ?= 1.2024.7
PLANTUML_JAR     ?= /tmp/plantuml.jar
PLANTUML_URL     ?= https://repo1.maven.org/maven2/net/sourceforge/plantuml/plantuml/$(PLANTUML_VERSION)/plantuml-$(PLANTUML_VERSION).jar

PUMl := $(wildcard puml/*.puml)
RENDERED := $(patsubst puml/%.puml,figures/%.svg,$(PUMl))
HAND_AUTHORED := $(wildcard figures/kd*.svg)

.PHONY: figures validate links check clean plantuml

figures: plantuml $(RENDERED)

$(RENDERED): figures/%.svg: puml/%.puml
	java -jar $(PLANTUML_JAR) -tsvg -failfast2 -charset UTF-8 -o ../figures $<

plantuml:
	@test -f $(PLANTUML_JAR) || curl -sS -L -o $(PLANTUML_JAR) $(PLANTUML_URL)

validate:
	@for f in $(RENDERED) $(HAND_AUTHORED); do \
		xmllint --noout "$$f" && echo "ok  $$f" || exit 1; \
	done

links:
	@python3 check-links.py README.md solution-architecture.md control-plane-operating-model.md delivery-design-pack.md data-plane-operations-guide.md ../steward-guide.md

check: validate links

clean:
	rm -f $(RENDERED)
