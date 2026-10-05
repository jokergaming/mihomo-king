package config

import (
	"fmt"
	"maps"
)

const ChainNodeName = "mihomo-king Chain"

func (s *Settings) ActiveChain() *ProxyChain {
	if s.Chain != nil && s.Chain.Subscription == s.Active {
		return s.Chain
	}
	return nil
}

// applyChain adds a separate exit node, leaving the original exit unchanged.
// The chain is offered only by manual groups; selecting it is an explicit
// routing change and automatic groups keep their original members.
func (s *Settings) applyChain(root map[string]any) error {
	chain := s.ActiveChain()
	if chain == nil {
		return nil
	}
	if chain.Entry == "" || chain.Exit == "" || chain.Entry == chain.Exit {
		return fmt.Errorf("chain requires two different nodes (entry, exit)")
	}

	proxies := anySlice(root["proxies"])
	byName := make(map[string]map[string]any, len(proxies))
	for _, value := range proxies {
		if proxy, ok := value.(map[string]any); ok {
			byName[str(proxy["name"])] = proxy
		}
	}
	if byName[ChainNodeName] != nil {
		return fmt.Errorf("node name %q is reserved for the chain", ChainNodeName)
	}
	if byName[chain.Entry] == nil {
		return fmt.Errorf("chain entry node %q is missing from the active config", chain.Entry)
	}
	exit := byName[chain.Exit]
	if exit == nil {
		return fmt.Errorf("chain exit node %q is missing from the active config", chain.Exit)
	}

	groups := anySlice(root["proxy-groups"])
	selectable := 0
	for _, value := range groups {
		group, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if str(group["name"]) == ChainNodeName {
			return fmt.Errorf("group name %q is reserved for the chain", ChainNodeName)
		}
		if isSelectorGroup(group) {
			group["proxies"] = appendUniqueStrings(stringList(group["proxies"]), ChainNodeName)
			selectable++
		}
	}
	if selectable == 0 {
		return fmt.Errorf("chain needs a select-type proxy group")
	}

	chained := maps.Clone(exit)
	chained["name"] = ChainNodeName
	chained["dialer-proxy"] = chain.Entry
	root["proxies"] = append(proxies, chained)
	return nil
}
