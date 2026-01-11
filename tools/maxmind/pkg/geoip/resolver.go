package geoip

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/metrodb"
	"github.com/oschwald/geoip2-golang"
)

type Record struct {
	IP                  net.IP
	CountryCode         string
	Country             string
	Region              string
	City                string
	CityID              int
	MetroName           string
	Latitude            float64
	Longitude           float64
	PostalCode          string
	TimeZone            string
	AccuracyRadius      int
	ASN                 uint
	ASNOrg              string
	IsAnycast           bool
	IsAnonymousProxy    bool
	IsSatelliteProvider bool
}

type Resolver interface {
	Resolve(ip net.IP) *Record
}

type resolver struct {
	log *slog.Logger

	cityDB  *geoip2.Reader
	asnDB   *geoip2.Reader
	metroDB *metrodb.MetroDB
}

func NewResolver(log *slog.Logger, cityDB *geoip2.Reader, asnDB *geoip2.Reader, metroDB *metrodb.MetroDB) (*resolver, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	if cityDB == nil {
		return nil, fmt.Errorf("cityDB is nil")
	}
	if asnDB == nil {
		return nil, fmt.Errorf("asnDB is nil")
	}
	if metroDB == nil {
		return nil, fmt.Errorf("metroDB is nil")
	}
	return &resolver{
		log:     log,
		cityDB:  cityDB,
		asnDB:   asnDB,
		metroDB: metroDB,
	}, nil
}

func (r *resolver) Resolve(ip net.IP) *Record {
	if ip == nil {
		return nil
	}

	if r.cityDB == nil && r.asnDB == nil {
		return nil
	}

	var (
		countryCode, country, region, city, postalCode, timeZone string
		cityID, accuracyRadius                                   int
		lat, lon                                                 float64
		asnNum                                                   uint
		asnOrg                                                   string
		isAnycast, isAnonymousProxy, isSatelliteProvider         bool
	)

	if r.cityDB != nil {
		rec, err := r.cityDB.City(ip)
		if err != nil {
			r.log.Debug("solana: geoip city lookup failed", "ip", ip.String(), "error", err)
		} else {
			if rec.Country.IsoCode != "" {
				countryCode = rec.Country.IsoCode
			}
			if rec.Country.Names["en"] != "" {
				country = rec.Country.Names["en"]
			}
			if len(rec.Subdivisions) > 0 {
				region = rec.Subdivisions[0].Names["en"]
			}
			if rec.City.GeoNameID != 0 {
				cityID = int(rec.City.GeoNameID)
			}
			if rec.City.Names["en"] != "" {
				city = rec.City.Names["en"]
			}
			lat = rec.Location.Latitude
			lon = rec.Location.Longitude
			postalCode = rec.Postal.Code
			timeZone = rec.Location.TimeZone
			accuracyRadius = int(rec.Location.AccuracyRadius)
			isAnycast = rec.Traits.IsAnycast
			isAnonymousProxy = rec.Traits.IsAnonymousProxy
			isSatelliteProvider = rec.Traits.IsSatelliteProvider
		}
	}

	if r.asnDB != nil {
		rec, err := r.asnDB.ASN(ip)
		if err != nil {
			r.log.Debug("solana: geoip asn lookup failed", "ip", ip.String(), "error", err)
		} else {
			asnNum = rec.AutonomousSystemNumber
			asnOrg = rec.AutonomousSystemOrganization
		}
	}

	var metroName string
	if r.metroDB != nil {
		metroName, _ = r.metroDB.Lookup(city, countryCode)
	}
	if metroName == "" {
		metroName = "Unknown"
	}

	if country == "" && asnNum == 0 {
		return nil
	}

	return &Record{
		IP:                  ip,
		CountryCode:         countryCode,
		Country:             country,
		Region:              region,
		City:                city,
		CityID:              cityID,
		MetroName:           metroName,
		Latitude:            lat,
		Longitude:           lon,
		PostalCode:          postalCode,
		TimeZone:            timeZone,
		AccuracyRadius:      accuracyRadius,
		ASN:                 asnNum,
		ASNOrg:              asnOrg,
		IsAnycast:           isAnycast,
		IsAnonymousProxy:    isAnonymousProxy,
		IsSatelliteProvider: isSatelliteProvider,
	}
}
