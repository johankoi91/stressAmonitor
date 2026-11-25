import { App as AntApp, ConfigProvider, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import type { ReactNode } from "react";
export default function ConsoleTheme({ children }: { children: ReactNode }) {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.darkAlgorithm,
        token: {
          colorPrimary: "#70aaff",
          colorInfo: "#70aaff",
          colorSuccess: "#69d7ad",
          colorWarning: "#f1c27b",
          colorError: "#ff939d",
          colorBgBase: "#0b121d",
          colorBgContainer: "#141f2e",
          colorBgElevated: "#1c2b3f",
          colorText: "#e5edf8",
          colorTextSecondary: "#a2b3ca",
          colorTextTertiary: "#8fa3bd",
          colorBorder: "#526b86",
          colorBorderSecondary: "#2b3c51",
          borderRadius: 8,
          fontSize: 14,
          controlHeight: 40,
          controlHeightSM: 34,
          fontFamily:
            'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
        },
        components: {
          Layout: {
            siderBg: "#101b2b",
            bodyBg: "#0c1420",
            headerBg: "#101b2b",
          },
          Menu: {
            darkItemBg: "#101b2b",
            darkItemSelectedBg: "#213c60",
            darkItemSelectedColor: "#bed9ff",
            darkItemHoverBg: "#1a2c44",
            itemHeight: 44,
          },
          Card: { headerBg: "transparent", headerFontSize: 15, paddingLG: 20 },
          Table: {
            headerBg: "#1c2c40",
            headerColor: "#b5c8e0",
            rowHoverBg: "#1b2e46",
            cellPaddingBlock: 13,
            cellPaddingInline: 12,
          },
          Form: { labelColor: "#bacbe1", labelFontSize: 13 },
          Tabs: {
            itemSelectedColor: "#9cc6ff",
            itemActiveColor: "#bed9ff",
            inkBarColor: "#70aaff",
          },
          Input: { activeShadow: "0 0 0 2px rgba(112,170,255,.13)" },
          InputNumber: { activeShadow: "0 0 0 2px rgba(112,170,255,.13)" },
        },
      }}
    >
      <AntApp>{children}</AntApp>
    </ConfigProvider>
  );
}
