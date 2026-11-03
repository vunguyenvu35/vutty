/**
 * Donations. Crypto (USDT) is hidden for now: set SHOW_CRYPTO_DONATE to true to show the addresses again in
 * Settings → Account → Donate and in Help → Support Vutty. The README block is kept in an HTML comment
 * marked CRYPTO-DONATE, and the QR codes stay in frontend/public/donate and docs/donate.
 */
export const SHOW_CRYPTO_DONATE = false

export const KOFI = 'https://ko-fi.com/vunguyenvu'

export const DONATE_NETWORKS = [
  {id: 'tron', name: 'USDT · Tron', chain: 'TRC20', address: 'TMXXHRNG7qauoFWJKYbqEbd4FGgt3DgjBK', qr: '/donate/usdt-tron.png'},
  {id: 'bsc', name: 'USDT · BSC', chain: 'BEP20', address: '0x4bdfd1d6456c28d06e3f8456ddac59e6168e5153', qr: '/donate/usdt-bsc.png'},
]
