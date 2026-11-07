export const ontTables: Record<string, string> = {
  DevInfo: '设备信息',
  WANC: '业务连接',
  TelnetCfg: '远程终端访问',
  SambaCfg: '文件共享',
  MgtServer: '运营商远程管理',
}
export const ontFields: Record<string, string> = {
  ModelName: '设备型号',
  HardwareVer: '硬件版本',
  SoftwareVer: '软件版本',
  Enable: '连接开关',
  ConnType: '连接方式',
  VLANID: '业务 VLAN',
  Priority: '802.1p 优先级',
  StrServList: '业务类型',
  Lan_Enable: '局域网 Telnet',
  Wan_Enable: '外网 Telnet',
  EnableSmb: 'SMB 文件共享',
  Tr069Enable: 'TR-069 远程管理',
}
export const fecLabels: Record<string, string> = {
  FECSeconds: '发生纠错的秒数',
  correctedBytes: '已纠正字节数',
  totalCodeWords: '累计码字数',
  correctedCodeWords: '已纠正码字数',
  uncorrectableCodeWords: '不可纠正码字数',
}
export function ontValue(key: string, value: string) {
  if (['Enable', 'Lan_Enable', 'Wan_Enable', 'EnableSmb', 'Tr069Enable'].includes(key))
    return value === '1' ? '已开启' : value === '0' ? '已关闭' : `未识别（${value}）`
  if (key === 'ConnType') return value === '4' ? '桥接' : `类型代码 ${value}`
  if (key === 'StrServList')
    return value
      .split(',')
      .map(
        (v) =>
          ({ INTERNET: '上网业务', TR069: '运营商远程管理', VOIP: '固定电话', IPTV: '网络电视' })[
            v
          ] || v,
      )
      .join('、')
  return value
}
export function ponLabel(value?: string) {
  return value?.toUpperCase() === 'O5' ? '已注册运行（O5）' : value ? `注册状态 ${value}` : '未读取'
}
