import { describe, test, expect } from "bun:test";
import { readFileSync } from "fs";
import path from "path";
import {
  locationStatusString,
  exchangeStatusString,
  deviceDeviceTypeString,
  deviceStatusString,
  deviceHealthString,
  deviceDesiredStatusString,
  interfaceStatusString,
  interfaceTypeString,
  loopbackTypeString,
  interfaceCYOAString,
  interfaceDIAString,
  routingModeString,
  linkLinkTypeString,
  linkStatusString,
  linkHealthString,
  linkDesiredStatusString,
  contributorStatusString,
  userUserTypeString,
  cyoaTypeString,
  userStatusString,
  multicastGroupStatusString,
  accessPassTypeTagString,
  accessPassStatusString,
} from "./state";

const fixtureData: Record<string, Record<string, string>> = JSON.parse(
  readFileSync(path.resolve(__dirname, "../../testdata/enum_strings.json"), "utf-8"),
);

const fnMap: Record<string, (v: number) => string> = {
  LocationStatus: locationStatusString,
  ExchangeStatus: exchangeStatusString,
  DeviceDeviceType: deviceDeviceTypeString,
  DeviceStatus: deviceStatusString,
  DeviceHealth: deviceHealthString,
  DeviceDesiredStatus: deviceDesiredStatusString,
  InterfaceStatus: interfaceStatusString,
  InterfaceType: interfaceTypeString,
  LoopbackType: loopbackTypeString,
  InterfaceCYOA: interfaceCYOAString,
  InterfaceDIA: interfaceDIAString,
  RoutingMode: routingModeString,
  LinkLinkType: linkLinkTypeString,
  LinkStatus: linkStatusString,
  LinkHealth: linkHealthString,
  LinkDesiredStatus: linkDesiredStatusString,
  ContributorStatus: contributorStatusString,
  UserUserType: userUserTypeString,
  CyoaType: cyoaTypeString,
  UserStatus: userStatusString,
  MulticastGroupStatus: multicastGroupStatusString,
  AccessPassTypeTag: accessPassTypeTagString,
  AccessPassStatus: accessPassStatusString,
};

describe("enum string functions", () => {
  for (const [enumName, cases] of Object.entries(fixtureData)) {
    const fn = fnMap[enumName];
    if (!fn) {
      continue;
    }
    describe(enumName, () => {
      for (const [value, expected] of Object.entries(cases)) {
        test(`${enumName}(${value}) === "${expected}"`, () => {
          expect(fn(Number(value))).toBe(expected);
        });
      }
    });
  }
});
