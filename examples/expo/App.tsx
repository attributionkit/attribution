import { useState } from 'react';
import { Button, SafeAreaView, Text, View } from 'react-native';
import {
  conversionValue,
  record,
  runtimeVersion,
  schemaHash,
  type AttributionUpdateReport,
} from '@attributionkit/expo';

export default function App() {
  const [report, setReport] = useState<AttributionUpdateReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const recordInstall = async () => {
    try {
      setError(null);
      const nextReport = await record('install');
      setReport(nextReport);
      // The example emits the exact AttributionUpdateReport as one JSON line so
      // it can be saved and imported by the local-only CLI probe.
      console.log(JSON.stringify(nextReport));
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    }
  };

  return (
    <SafeAreaView style={{ flex: 1 }}>
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 }}>
        <Text accessibilityRole="header" style={{ fontSize: 24, fontWeight: '700' }}>
          AttributionKit Expo Probe
        </Text>
        <Text accessibilityLabel={`Runtime ${runtimeVersion}`}>Runtime {runtimeVersion}</Text>
        <Text accessibilityLabel={`Schema ${schemaHash()}`}>Schema {schemaHash().slice(0, 12)}…</Text>
        <Text>Install value {conversionValue('install')}</Text>
        <Button title="Record Install" onPress={recordInstall} />
        {report ? (
          <>
            <Text accessibilityLabel={`Last event ${report.event}`}>
              {report.event}: AAK {report.adAttributionKit.status} · SKAN {report.skAdNetwork.status}
            </Text>
            <Text selectable accessibilityLabel="Copyable AttributionKit probe report">
              {JSON.stringify(report)}
            </Text>
          </>
        ) : null}
        {error ? <Text accessibilityRole="alert">{error}</Text> : null}
      </View>
    </SafeAreaView>
  );
}
