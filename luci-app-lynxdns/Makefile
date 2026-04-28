include $(TOPDIR)/rules.mk

LUCI_TITLE:=LynxDNS - Intelligent DNS Resolver Web UI
LUCI_DEPENDS:=+lynxdns +luci-base +luci-lib-base +luci-lib-jsonc +curl
LUCI_PKGARCH:=all

include $(TOPDIR)/feeds/luci/luci.mk

$(eval $(call BuildPackage,luci-app-lynxdns))
