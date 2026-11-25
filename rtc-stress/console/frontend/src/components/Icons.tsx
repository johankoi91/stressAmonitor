import {
  AppstoreOutlined,
  AudioOutlined,
  ArrowRightOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloudServerOutlined,
  ControlOutlined,
  DeleteOutlined,
  DownOutlined,
  DownloadOutlined,
  ExclamationCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  StopOutlined,
  UploadOutlined,
  PlusOutlined,
  SettingOutlined,
} from "@ant-design/icons";
const icons = {
  grid: AppstoreOutlined,
  audio: AudioOutlined,
  server: CloudServerOutlined,
  tune: ControlOutlined,
  arrow: ArrowRightOutlined,
  check: CheckOutlined,
  clock: ClockCircleOutlined,
  trash: DeleteOutlined,
  chevron: DownOutlined,
  download: DownloadOutlined,
  alert: ExclamationCircleOutlined,
  play: PlayCircleOutlined,
  refresh: ReloadOutlined,
  stop: StopOutlined,
  upload: UploadOutlined,
  plus: PlusOutlined,
  settings: SettingOutlined,
};
export function Icon({ name, size = 18 }: { name: string; size?: number }) {
  const Component = icons[name as keyof typeof icons] || AppstoreOutlined;
  return <Component style={{ fontSize: size }} aria-hidden />;
}
