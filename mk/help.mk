.PHONY: help

help: pad = 24 # padding for two columns
help:	## Help
	@echo "Commands:"
	@grep -E -h '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	 | sort -k 1 \
	 | awk -v pad="$(pad)" 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-*s\033[0m %s\n", pad, $$1, $$2}'
	@echo

